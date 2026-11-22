// The SoundCloud Widget API behind the Player interface. Public tracks play
// in full with no app registration. Go+ tracks play as a preview.

import { extrapolate, loadScript, type Player, type PlayerFactory } from "./player.ts";

interface Widget {
  bind(event: string, cb: (e?: { currentPosition?: number }) => void): void;
  load(url: string, opts: Record<string, unknown>): void;
  play(): void;
  pause(): void;
  seekTo(ms: number): void;
  getDuration(cb: (ms: number) => void): void;
}

interface SCNamespace {
  Widget: ((iframe: HTMLIFrameElement) => Widget) & {
    Events: Record<"READY" | "PLAY" | "PAUSE" | "FINISH" | "PLAY_PROGRESS" | "ERROR", string>;
  };
}

declare global {
  interface Window {
    SC?: SCNamespace;
  }
}

const WIDGET = "https://w.soundcloud.com/player/";

// The room shows its own title and controls, so the widget hides extras.
const OPTIONS = {
  auto_play: true,
  visual: true,
  show_comments: false,
  show_reposts: false,
  show_teaser: false,
  hide_related: true,
  buying: false,
  sharing: false,
  download: false,
};

function trackUrl(id: string): string {
  return `https://api.soundcloud.com/tracks/${encodeURIComponent(id)}`;
}

/** How far into a track a start must be before the widget is told to seek. */
const SEEK_MIN_MS = 1_500;

export const createSoundCloudPlayer: PlayerFactory = async (mount, events) => {
  await loadScript(`${WIDGET}api.js`, "The SoundCloud player could not be loaded.");
  const SC = window.SC;
  if (!SC) throw new Error("The SoundCloud player could not be loaded.");
  const E = SC.Widget.Events;

  let widget: Widget | null = null;
  let ready = false;
  // The track the room wants, and the one the widget was last given.
  let wantUrl: string | null = null;
  let givenUrl: string | null = null;
  let startAtMs: number | null = null;
  let started = false;
  let last = { playing: false, positionMs: 0, durationMs: 0, atMs: 0 };

  const fetchDuration = () =>
    widget?.getDuration((ms) => {
      last = { ...last, durationMs: ms };
    });

  const bind = (w: Widget) => {
    w.bind(E.READY, () => {
      ready = true;
      // The room moved on while the widget was starting.
      if (wantUrl && wantUrl !== givenUrl) {
        givenUrl = wantUrl;
        w.load(wantUrl, { ...OPTIONS, callback: fetchDuration });
        return;
      }
      fetchDuration();
    });
    w.bind(E.PLAY, () => {
      last = { ...last, playing: true, atMs: performance.now() };
      if (!started) {
        started = true;
        if (startAtMs !== null && startAtMs > SEEK_MIN_MS) w.seekTo(startAtMs);
        startAtMs = null;
        fetchDuration();
      }
    });
    w.bind(E.PAUSE, () => {
      last = { ...last, playing: false };
    });
    w.bind(E.PLAY_PROGRESS, (e) => {
      last = {
        ...last,
        positionMs: e?.currentPosition ?? last.positionMs,
        atMs: performance.now(),
      };
    });
    w.bind(E.FINISH, () => {
      last = { ...last, playing: false };
      events.onEnded();
    });
    w.bind(E.ERROR, () => events.onError());
  };

  const player: Player = {
    load(trackId, startMs) {
      startAtMs = startMs;
      started = false;
      last = { playing: false, positionMs: 0, durationMs: 0, atMs: performance.now() };
      const url = trackUrl(trackId);
      wantUrl = url;
      if (widget && ready) {
        givenUrl = url;
        widget.load(url, { ...OPTIONS, callback: fetchDuration });
        return;
      }
      if (widget) return;
      givenUrl = url;
      const iframe = document.createElement("iframe");
      iframe.title = "SoundCloud player";
      iframe.allow = "autoplay; encrypted-media";
      iframe.src = `${WIDGET}?${new URLSearchParams({ url, ...stringify(OPTIONS) })}`;
      mount.append(iframe);
      widget = SC.Widget(iframe);
      bind(widget);
    },
    play() {
      widget?.play();
    },
    pause() {
      widget?.pause();
    },
    seek(ms) {
      widget?.seekTo(ms);
      last = { ...last, positionMs: ms, atMs: performance.now() };
    },
    stop() {
      widget?.pause();
      started = false;
      last = { ...last, playing: false };
    },
    state() {
      return {
        playing: last.playing,
        buffering: false,
        positionMs: extrapolate(last.positionMs, last.atMs, last.playing, performance.now()),
        durationMs: last.durationMs,
      };
    },
    destroy() {
      mount.replaceChildren();
      widget = null;
    },
  };
  return player;
};

function stringify(opts: Record<string, boolean>): Record<string, string> {
  return Object.fromEntries(Object.entries(opts).map(([k, v]) => [k, String(v)]));
}
