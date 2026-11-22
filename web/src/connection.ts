import { hasSession } from "./api.ts";
import { ClockSync } from "./clock.ts";
import type { Command, Room, ServerMessage } from "./protocol.ts";

export type Status = "connecting" | "open" | "reconnecting";

/** Why the server closed the room to this member for good. */
export type Closed = "kicked" | "ended" | "gone" | "too_many_tabs";

export interface Handlers {
  onState(room: Room, joinSecret: string | undefined): void;
  onError(message: string): void;
  onStatus(status: Status): void;
  onClosed(reason: Closed): void;
}

const FINAL: readonly string[] = ["kicked", "ended", "gone", "too_many_tabs"];
const PING_EVERY_MS = 10_000;

/** A room WebSocket that reconnects and keeps a server clock estimate. */
export class Connection {
  #roomId: string;
  #handlers: Handlers;
  #ws: WebSocket | null = null;
  #clock = new ClockSync();
  #attempt = 0;
  #stopped = false;
  #ping: ReturnType<typeof setInterval> | undefined;
  #retry: ReturnType<typeof setTimeout> | undefined;

  constructor(roomId: string, handlers: Handlers) {
    this.#roomId = roomId;
    this.#handlers = handlers;
    this.#open();
  }

  /** The server's clock, in epoch milliseconds. */
  serverNow(): number {
    return this.#clock.serverNow(Date.now());
  }

  send(cmd: Command): void {
    if (this.#ws?.readyState === WebSocket.OPEN) this.#ws.send(JSON.stringify(cmd));
  }

  close(): void {
    this.#stopped = true;
    clearInterval(this.#ping);
    clearTimeout(this.#retry);
    this.#ws?.close();
  }

  #open(): void {
    const scheme = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(
      `${scheme}://${location.host}/api/rooms/${encodeURIComponent(this.#roomId)}/ws`,
    );
    this.#ws = ws;
    let opened = false;

    ws.onopen = () => {
      opened = true;
      this.#attempt = 0;
      this.#handlers.onStatus("open");
      this.#sendPing();
      this.#ping = setInterval(() => this.#sendPing(), PING_EVERY_MS);
    };
    ws.onmessage = (event) => {
      const msg = JSON.parse(String(event.data)) as ServerMessage;
      if (msg.type === "state") {
        this.#clock.seed(msg.server_ms, Date.now());
        this.#handlers.onState(msg.room, msg.join_secret);
      } else if (msg.type === "pong") {
        this.#clock.sample(msg.t0, Date.now(), msg.server_ms);
      } else {
        this.#handlers.onError(msg.message);
      }
    };
    ws.onclose = (event) => {
      clearInterval(this.#ping);
      if (this.#stopped) return;
      if (FINAL.includes(event.reason)) {
        this.#stopped = true;
        this.#handlers.onClosed(event.reason as Closed);
        return;
      }
      void this.#reconnect(opened);
    };
  }

  async #reconnect(wasOpen: boolean): Promise<void> {
    // A socket that never opened may mean the session is gone. A failed
    // session check that is itself a network error means we are offline.
    if (!wasOpen) {
      const alive = await hasSession(this.#roomId).catch(() => true);
      if (!alive) {
        this.#stopped = true;
        this.#handlers.onClosed("gone");
        return;
      }
    }
    if (this.#stopped) return;
    this.#handlers.onStatus("reconnecting");
    const delay = Math.min(8_000, 500 * 2 ** this.#attempt++);
    this.#retry = setTimeout(() => this.#open(), delay);
  }

  #sendPing(): void {
    this.send({ type: "ping", t0: Date.now() });
  }
}
