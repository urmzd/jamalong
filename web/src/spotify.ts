// The Spotify embed behind the Player interface. Playback runs inside
// Spotify's own embed, so the listener's Spotify login stays in their
// browser: signed in, they hear the full track; signed out, a preview.
//
// This talks to the embed with the same postMessage commands as Spotify's
// iFrame API. That script is a development build that needs 'unsafe-eval',
// which this page's content security policy refuses.

import { extrapolate, type Player, type PlayerFactory } from "./player.ts";

const ORIGIN = "https://open.spotify.com";

interface PlaybackState {
  isPaused: boolean;
  isBuffering: boolean;
  duration: number;
  position: number;
}

type EmbedMessage =
  | { type: "ready" }
  | { type: "playback_update"; payload?: unknown }
  | { type: "error"; payload?: unknown };

type Command =
  | { command: "load_complete_ack" | "play" | "pause" | "resume" }
  | { command: "seek"; timestamp: number };

/** How close to the end a stop counts as the track finishing. */
const END_SLACK_MS = 1_500;

export const createSpotifyPlayer: PlayerFactory = async (mount, events) => {
  const iframe = document.createElement("iframe");
  iframe.title = "Spotify player";
  iframe.allow = "autoplay; clipboard-write; encrypted-media; fullscreen; picture-in-picture";

  // Commands wait until the embed says it is ready.
  let loading = false;
  let queue: Command[] = [];
  let started = false;
  let ended = false;
  let last = { playing: false, buffering: false, positionMs: 0, durationMs: 0, atMs: 0 };

  const post = (cmd: Command) => iframe.contentWindow?.postMessage(cmd, ORIGIN);
  const send = (cmd: Command) => {
    if (loading) queue.push(cmd);
    else post(cmd);
  };

  const onUpdate = (data: PlaybackState) => {
    const playing = !data.isPaused;
    const wasPlaying = last.playing;
    last = {
      playing,
      buffering: data.isBuffering,
      positionMs: data.position,
      durationMs: data.duration,
      atMs: performance.now(),
    };
    if (playing) started = true;
    // The embed reports the end in one of two ways. A preview, and a full
    // track in our runs, sends position equal to duration while still
    // "playing" and then goes quiet. Other builds pause at, or jump back
    // from, the end.
    const reachedEnd = data.duration > 0 && data.position >= data.duration;
    const pausedAtEnd =
      wasPlaying &&
      !playing &&
      ((data.duration > 0 && data.position >= data.duration - END_SLACK_MS) || data.position === 0);
    if (started && !ended && (reachedEnd || pausedAtEnd)) {
      ended = true;
      last = { ...last, playing: false };
      events.onEnded();
    }
  };

  const onMessage = (e: MessageEvent) => {
    if (e.origin !== ORIGIN || e.source !== iframe.contentWindow) return;
    const msg = e.data as EmbedMessage | null;
    switch (msg?.type) {
      case "ready": {
        loading = false;
        post({ command: "load_complete_ack" });
        const pending = queue;
        queue = [];
        for (const cmd of pending) post(cmd);
        break;
      }
      case "playback_update":
        if (isPlaybackState(msg.payload)) onUpdate(msg.payload);
        break;
      case "error":
        events.onError();
        break;
    }
  };
  window.addEventListener("message", onMessage);
  mount.append(iframe);

  const player: Player = {
    load(trackId, startMs) {
      const url = new URL(`${ORIGIN}/embed/track/${encodeURIComponent(trackId)}`);
      url.searchParams.set("utm_source", "iframe-api");
      const startS = Math.floor(startMs / 1000);
      if (startS > 0) url.searchParams.set("t", String(startS));
      loading = true;
      queue = [{ command: "play" }];
      started = false;
      ended = false;
      last = {
        playing: false,
        buffering: true,
        positionMs: startMs,
        durationMs: 0,
        atMs: performance.now(),
      };
      iframe.src = url.href;
    },
    play() {
      send({ command: started ? "resume" : "play" });
    },
    pause() {
      send({ command: "pause" });
    },
    seek(ms) {
      send({ command: "seek", timestamp: ms / 1000 });
    },
    stop() {
      send({ command: "pause" });
      last = { ...last, playing: false };
    },
    state() {
      return {
        playing: last.playing,
        buffering: last.buffering,
        positionMs: clampTo(
          extrapolate(last.positionMs, last.atMs, last.playing, performance.now()),
          last.durationMs,
        ),
        durationMs: last.durationMs,
      };
    },
    destroy() {
      window.removeEventListener("message", onMessage);
      iframe.remove();
    },
  };
  return player;
};

function clampTo(ms: number, durationMs: number): number {
  return durationMs > 0 ? Math.min(ms, durationMs) : ms;
}

// playback_started carries only the track URI, and a message from a newer
// embed may change shape, so only a complete state is used.
function isPlaybackState(v: unknown): v is PlaybackState {
  const p = v as Partial<PlaybackState> | null;
  return (
    typeof p?.isPaused === "boolean" && Number.isFinite(p.position) && Number.isFinite(p.duration)
  );
}
