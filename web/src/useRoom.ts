import { useCallback, useEffect, useRef, useState } from "react";
import { type Closed, Connection, type Status } from "./connection.ts";
import type { Command, Room } from "./protocol.ts";

export interface RoomSession {
  room: Room | null;
  /** Present for the host only. */
  joinSecret: string | undefined;
  status: Status;
  closed: Closed | null;
  /** The latest error from the server, cleared after a few seconds. */
  error: string | null;
  send(cmd: Command): void;
  serverNow(): number;
}

const ERROR_MS = 5_000;

/** Server errors arrive as lowercase fragments. Show them as a sentence. */
function sentence(text: string): string {
  const s = text.trim();
  if (!s) return "Something went wrong.";
  return s.charAt(0).toUpperCase() + s.slice(1) + (/[.!?]$/.test(s) ? "" : ".");
}

export function useRoom(roomId: string): RoomSession {
  const [room, setRoom] = useState<Room | null>(null);
  const [joinSecret, setJoinSecret] = useState<string>();
  const [status, setStatus] = useState<Status>("connecting");
  const [closed, setClosed] = useState<Closed | null>(null);
  const [error, setError] = useState<string | null>(null);
  const conn = useRef<Connection | null>(null);

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const c = new Connection(roomId, {
      onState: (next, secret) => {
        setRoom(next);
        setJoinSecret(secret);
      },
      onError: (message) => {
        setError(sentence(message));
        clearTimeout(timer);
        timer = setTimeout(() => setError(null), ERROR_MS);
      },
      onStatus: setStatus,
      onClosed: setClosed,
    });
    conn.current = c;
    return () => {
      clearTimeout(timer);
      c.close();
    };
  }, [roomId]);

  const send = useCallback((cmd: Command) => conn.current?.send(cmd), []);
  const serverNow = useCallback(() => conn.current?.serverNow() ?? Date.now(), []);
  return { room, joinSecret, status, closed, error, send, serverNow };
}
