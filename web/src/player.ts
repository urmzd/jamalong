// One interface over every provider's embedded player, so the speaker can
// keep any of them on the room clock.

export interface PlayerState {
  playing: boolean;
  buffering: boolean;
  positionMs: number;
  /** 0 until the player knows the length. */
  durationMs: number;
}

export interface Player {
  /** Loads a track and starts it at startMs. */
  load(trackId: string, startMs: number): void;
  play(): void;
  pause(): void;
  seek(ms: number): void;
  stop(): void;
  state(): PlayerState;
  destroy(): void;
}

export interface PlayerEvents {
  /** The track reached its end. */
  onEnded(): void;
  /** The track cannot play here. */
  onError(): void;
}

/** Creates a player inside mount and resolves once it takes commands. */
export type PlayerFactory = (mount: HTMLElement, events: PlayerEvents) => Promise<Player>;

const scripts = new Map<string, Promise<void>>();

/** Adds a provider's script to the page once. */
export function loadScript(src: string, failure: string): Promise<void> {
  let loading = scripts.get(src);
  if (!loading) {
    loading = new Promise<void>((resolve, reject) => {
      const script = document.createElement("script");
      script.src = src;
      script.async = true;
      script.onload = () => resolve();
      script.onerror = () => {
        scripts.delete(src);
        script.remove();
        reject(new Error(failure));
      };
      document.head.append(script);
    });
    scripts.set(src, loading);
  }
  return loading;
}

/** Estimates where a player is now from its last progress event. */
export function extrapolate(
  positionMs: number,
  atMs: number,
  playing: boolean,
  nowMs: number,
): number {
  return playing ? positionMs + Math.max(0, nowMs - atMs) : positionMs;
}
