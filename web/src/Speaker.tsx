import { useEffect, useRef } from "react";
import { positionMs } from "./clock.ts";
import type { Player, PlayerFactory } from "./player.ts";
import { createSoundCloudPlayer } from "./soundcloud.ts";
import { createSpotifyPlayer } from "./spotify.ts";
import { decide, FOLLOW_TOLERANCE_MS } from "./sync.ts";
import type { RoomSession } from "./useRoom.ts";
import { createYouTubePlayer } from "./youtube.ts";

const TICK_MS = 500;
const REPORT_MS = 2_000;
/** How long a speaker waits before repeating a play or pause command. */
const COMMAND_REPEAT_MS = 3_000;
/** How long reports wait for a seek to land before they resume anyway. */
const SEEK_SETTLE_MS = 5_000;

const FACTORIES: Record<string, PlayerFactory> = {
  youtube: createYouTubePlayer,
  spotify: createSpotifyPlayer,
  soundcloud: createSoundCloudPlayer,
};

interface Slot {
  mount: HTMLDivElement;
  player: Player | null;
  failed: boolean;
}

/**
 * Plays the room's current track on this device and keeps it on the room
 * clock. When this device is the lead speaker it also reports its position,
 * so the clock follows what people actually hear.
 *
 * Each provider gets its own player, created the first time the room plays
 * one of its tracks. Only the player for the current track is shown.
 */
export function Speaker({ session }: { session: RoomSession }) {
  const holder = useRef<HTMLDivElement>(null);
  const latest = useRef(session);
  latest.current = session;

  useEffect(() => {
    const slots = new Map<string, Slot>();
    let disposed = false;
    let active: string | null = null;
    let loaded: string | null = null;
    let finished: string | null = null;
    let seenSeekSeq = 0;
    let lastReport = 0;
    // When play or pause was last sent. A player that is still buffering
    // restarts its buffering on a repeated command, so a repeat waits.
    // Each command kind has its own wait, so alternating commands cannot
    // reset each other's.
    const lastSent = new Map<string, number>();
    const due = (kind: "play" | "pause" | "seek") => {
      const at = Date.now();
      if (at - (lastSent.get(kind) ?? 0) < COMMAND_REPEAT_MS) return false;
      lastSent.set(kind, at);
      return true;
    };
    // Where the last seek went. Reports wait until the player gets there,
    // so a position read before the seek cannot undo it.
    let seekTarget: { ms: number; at: number } | null = null;
    // The lead has been within tolerance of the clock on this track.
    let leadSynced = false;
    // The last playing state sent to the log, to log changes only.
    let wasPlaying: boolean | null = null;

    // One line in the server log about what this device's player did.
    const note = (event: string, positionMs: number, durationMs: number) => {
      latest.current.send({
        type: "player",
        event,
        provider: active ?? "",
        item_id: loaded ?? "",
        position_ms: Math.round(positionMs),
        duration_ms: Math.round(durationMs),
      });
    };

    const isLead = () => {
      const room = latest.current.room;
      return !!room && room.lead === room.you;
    };
    // The end of a track, or a track that will not play, moves the queue on.
    // The reason and the player's position go to the server log.
    const reportEnded = (itemId: string | null, reason: string) => {
      finished = itemId;
      if (!itemId || !isLead()) return;
      const state = (active ? slots.get(active)?.player : null)?.state();
      latest.current.send({
        type: "report",
        item_id: itemId,
        state: "ended",
        reason,
        position_ms: Math.round(state?.positionMs ?? 0),
        duration_ms: Math.round(state?.durationMs ?? 0),
      });
    };

    // Returns the slot for a provider, starting its player on first use.
    const slotFor = (provider: string): Slot => {
      const slot = slots.get(provider);
      if (slot) return slot;
      const mount = document.createElement("div");
      mount.className = `player player-${provider}`;
      mount.hidden = true;
      holder.current?.append(mount);
      const created: Slot = { mount, player: null, failed: false };
      slots.set(provider, created);
      const factory = FACTORIES[provider];
      const events = {
        // Events from a player that is not on screen belong to a track the
        // room has left.
        onEnded: () => active === provider && reportEnded(loaded, `${provider}-ended`),
        onError: (code?: number) =>
          active === provider &&
          reportEnded(
            loaded,
            code === undefined ? `${provider}-error` : `${provider}-error-${code}`,
          ),
        onNote: (event: string, positionMs: number, durationMs: number) =>
          active === provider && note(event, positionMs, durationMs),
      };
      if (!factory) {
        created.failed = true;
      } else {
        factory(mount, events)
          .then((p) => {
            if (disposed) p.destroy();
            else created.player = p;
          })
          .catch(() => {
            created.failed = true;
          });
      }
      return created;
    };

    const show = (provider: string | null) => {
      for (const [name, slot] of slots) slot.mount.hidden = name !== provider;
    };

    const timer = setInterval(() => {
      const { room, serverNow, send } = latest.current;
      if (!room) return;
      const now = room.now;
      if (now && finished === now.item.id && now.seek_seq === seenSeekSeq) return;

      const current = active ? slots.get(active)?.player : null;
      const state = current?.state() ?? {
        playing: false,
        buffering: false,
        positionMs: 0,
        durationMs: 0,
      };
      const lead = room.lead === room.you;
      const actions = decide(
        now,
        serverNow(),
        { itemId: loaded, playing: state.playing, positionMs: state.positionMs },
        { lead, seenSeekSeq, leadSynced },
      );
      // A tick that loads a track has a snapshot of the previous one, so it
      // reports nothing.
      let switched = false;
      for (const action of actions) {
        if (action.kind === "load") {
          const slot = slotFor(action.provider);
          if (slot.failed) {
            // This device cannot play the provider. The lead skips the track;
            // a follower stays quiet until the next one.
            current?.stop();
            reportEnded(action.itemId, `${action.provider}-unavailable`);
            break;
          }
          if (!slot.player) break; // still starting; try again next tick
          if (active !== action.provider) current?.stop();
          active = action.provider;
          show(active);
          slot.player.load(action.trackId, action.startMs, action.durationMs);
          loaded = action.itemId;
          finished = null;
          switched = true;
          leadSynced = false;
          wasPlaying = null;
          lastSent.clear();
          lastSent.set("play", Date.now());
          seekTarget = null;
          note("load", action.startMs, action.durationMs);
          continue;
        }
        const player = active ? slots.get(active)?.player : null;
        if (!player) break;
        switch (action.kind) {
          case "stop":
            player.stop();
            loaded = null;
            break;
          case "seek": {
            // A seek someone asked for always goes. A correction for drift
            // waits like play and pause, since a player still applying the
            // last seek restarts its buffering on another.
            const asked = now?.seek_seq !== seenSeekSeq;
            if (!asked && !due("seek")) break;
            if (lead && !asked) note("resync", state.positionMs, state.durationMs);
            player.seek(action.toMs);
            seekTarget = { ms: action.toMs, at: Date.now() };
            finished = null;
            break;
          }
          case "play":
            if (due("play")) player.play();
            break;
          case "pause":
            if (due("pause")) player.pause();
            break;
        }
      }
      if (now) seenSeekSeq = now.seek_seq;
      if (now && !switched && loaded === now.item.id) {
        // The server may learn the length after load.
        current?.setLength?.(now.item.track.duration_ms);
        const drift = Math.abs(state.positionMs - positionMs(now, serverNow()));
        if (state.playing && drift <= FOLLOW_TOLERANCE_MS) leadSynced = true;
        if (wasPlaying !== null && wasPlaying !== state.playing) {
          note(state.playing ? "playing" : "paused", state.positionMs, state.durationMs);
        }
        wasPlaying = state.playing;
      }
      // What this speaker is doing, for tests and for anyone inspecting it.
      const stack = holder.current;
      if (stack) {
        stack.dataset.provider = active ?? "";
        stack.dataset.playing = String(state.playing);
        stack.dataset.positionMs = String(Math.round(state.positionMs));
      }

      // Only a player that has started reports, so a track still loading
      // does not pull the clock back to zero. After a seek, reports wait
      // until the player reaches the target, or give up after a while.
      if (seekTarget) {
        const landed = Math.abs(state.positionMs - seekTarget.ms) <= FOLLOW_TOLERANCE_MS;
        if (landed || Date.now() - seekTarget.at > SEEK_SETTLE_MS) seekTarget = null;
      }
      const reportDue = Date.now() - lastReport >= REPORT_MS;
      if (
        lead &&
        !switched &&
        !seekTarget &&
        state.playing &&
        now &&
        loaded === now.item.id &&
        now.paused_at_ms === null &&
        reportDue
      ) {
        lastReport = Date.now();
        send({
          type: "report",
          item_id: now.item.id,
          state: state.buffering ? "buffering" : "playing",
          position_ms: Math.round(state.positionMs),
          duration_ms: Math.round(state.durationMs),
        });
      }
    }, TICK_MS);

    return () => {
      disposed = true;
      clearInterval(timer);
      for (const slot of slots.values()) {
        slot.player?.destroy();
        slot.mount.remove();
      }
    };
  }, []);

  return <div className="player-stack" ref={holder} />;
}
