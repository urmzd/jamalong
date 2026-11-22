import type { Now } from "./protocol.ts";

/** Where the room clock is in the current track, in milliseconds. */
export function positionMs(now: Now, serverNowMs: number): number {
  const ref = now.paused_at_ms ?? serverNowMs;
  const pos = Math.max(0, ref - now.started_at_ms);
  const duration = now.item.track.duration_ms;
  return duration > 0 ? Math.min(pos, duration) : pos;
}

/**
 * Estimates the gap between this device's clock and the server's from
 * ping round trips. The sample with the shortest round trip wins, because its
 * midpoint is the least uncertain.
 */
export class ClockSync {
  #offset = 0;
  #bestRtt = Number.POSITIVE_INFINITY;
  #samples = 0;

  /** A coarse first guess from any server timestamp. */
  seed(serverMs: number, localNowMs: number): void {
    if (this.#samples === 0) this.#offset = serverMs - localNowMs;
  }

  /** One ping: sent at sentMs and answered at receivedMs, both local. */
  sample(sentMs: number, receivedMs: number, serverMs: number): void {
    const rtt = receivedMs - sentMs;
    if (rtt < 0) return;
    this.#samples++;
    // Let the best sample age, so a clock that drifts is corrected in time.
    this.#bestRtt *= 1.05;
    if (rtt <= this.#bestRtt) {
      this.#bestRtt = rtt;
      this.#offset = serverMs + rtt / 2 - receivedMs;
    }
  }

  serverNow(localNowMs: number): number {
    return localNowMs + this.#offset;
  }
}

/** Formats milliseconds as m:ss. */
export function formatTime(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}
