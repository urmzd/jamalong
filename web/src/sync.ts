import { positionMs } from "./clock.ts";
import type { Now } from "./protocol.ts";

/** What a speaker's player is doing right now. */
export interface PlayerSnapshot {
  /** The room item the player has loaded, or null. */
  itemId: string | null;
  playing: boolean;
  positionMs: number;
}

export type SyncAction =
  | { kind: "stop" }
  | {
      kind: "load";
      itemId: string;
      provider: string;
      trackId: string;
      startMs: number;
      durationMs: number;
    }
  | { kind: "pause" }
  | { kind: "play" }
  | { kind: "seek"; toMs: number };

/** How far a follower may drift from the room clock before it jumps. */
export const FOLLOW_TOLERANCE_MS = 1200;

/**
 * How far a lead that was on the clock may fall behind it before the lead
 * jumps back to the clock. A player that restarts a track on resume falls
 * this far at once; a buffering one does not.
 */
export const LEAD_RESYNC_MS = 5000;

/**
 * Decides what a speaker must do to match the room.
 *
 * A follower chases the room clock. The lead does not: the room clock chases
 * the lead, since the lead is the device everyone hears first. The lead jumps
 * on an explicit seek, which it sees as a new seek_seq, and when it was on
 * the clock and then fell far behind it, which is a player glitch.
 */
export function decide(
  now: Now | null,
  serverNowMs: number,
  player: PlayerSnapshot,
  opts: { lead: boolean; seenSeekSeq: number; leadSynced?: boolean },
): SyncAction[] {
  if (!now) return player.itemId ? [{ kind: "stop" }] : [];
  const expected = positionMs(now, serverNowMs);
  if (player.itemId !== now.item.id) {
    const load: SyncAction = {
      kind: "load",
      itemId: now.item.id,
      provider: now.item.track.provider,
      trackId: now.item.track.provider_id,
      startMs: expected,
      durationMs: now.item.track.duration_ms,
    };
    return now.paused_at_ms === null ? [load] : [load, { kind: "pause" }];
  }
  const actions: SyncAction[] = [];
  const sought = now.seek_seq !== opts.seenSeekSeq;
  const drift = Math.abs(player.positionMs - expected);
  const leadGlitch =
    opts.lead &&
    !!opts.leadSynced &&
    player.playing &&
    expected - player.positionMs > LEAD_RESYNC_MS;
  if (sought || (!opts.lead && drift > FOLLOW_TOLERANCE_MS) || leadGlitch) {
    actions.push({ kind: "seek", toMs: expected });
  }
  const shouldPlay = now.paused_at_ms === null;
  if (shouldPlay && !player.playing) actions.push({ kind: "play" });
  if (!shouldPlay && player.playing) actions.push({ kind: "pause" });
  return actions;
}
