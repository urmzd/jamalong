// A thin, typed layer over the YouTube IFrame Player API.

interface YTPlayer {
  loadVideoById(opts: { videoId: string; startSeconds: number }): void;
  playVideo(): void;
  pauseVideo(): void;
  stopVideo(): void;
  seekTo(seconds: number, allowSeekAhead: boolean): void;
  getCurrentTime(): number;
  getDuration(): number;
  getPlayerState(): number;
  destroy(): void;
}

interface YTNamespace {
  Player: new (
    el: HTMLElement,
    opts: {
      host: string;
      width: string;
      height: string;
      playerVars: Record<string, string | number>;
      events: {
        onReady(): void;
        onStateChange(e: { data: number }): void;
        onError(e: { data: number }): void;
      };
    },
  ) => YTPlayer;
}

declare global {
  interface Window {
    YT?: YTNamespace;
    onYouTubeIframeAPIReady?: () => void;
  }
}

export const YT_ENDED = 0;
export const YT_PLAYING = 1;
export const YT_BUFFERING = 3;

let loading: Promise<YTNamespace> | undefined;

function loadApi(): Promise<YTNamespace> {
  loading ??= new Promise((resolve, reject) => {
    if (window.YT?.Player) {
      resolve(window.YT);
      return;
    }
    window.onYouTubeIframeAPIReady = () => {
      if (window.YT) resolve(window.YT);
    };
    const script = document.createElement("script");
    script.src = "https://www.youtube.com/iframe_api";
    script.onerror = () => {
      loading = undefined;
      reject(new Error("The YouTube player could not be loaded."));
    };
    document.head.append(script);
  });
  return loading;
}

export interface PlayerEvents {
  onState(state: number): void;
  onError(code: number): void;
}

/** Creates a player inside el and resolves once it is ready for commands. */
export async function createPlayer(el: HTMLElement, events: PlayerEvents): Promise<YTPlayer> {
  const yt = await loadApi();
  return new Promise((resolve) => {
    const player = new yt.Player(el, {
      host: "https://www.youtube-nocookie.com",
      width: "100%",
      height: "100%",
      playerVars: {
        playsinline: 1,
        // The room owns playback, so the player's own controls stay off.
        controls: 0,
        disablekb: 1,
        rel: 0,
        origin: location.origin,
      },
      events: {
        onReady: () => resolve(player),
        onStateChange: (e) => events.onState(e.data),
        onError: (e) => events.onError(e.data),
      },
    });
  });
}

export type { YTPlayer };
