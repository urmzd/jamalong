import { useEffect, useRef } from "react";
import type { Player, PlayerFactory } from "./player.ts";
import { createSoundCloudPlayer } from "./soundcloud.ts";
import { createSpotifyPlayer } from "./spotify.ts";
import { decide } from "./sync.ts";
import type { RoomSession } from "./useRoom.ts";
import { createYouTubePlayer } from "./youtube.ts";

const TICK_MS = 500;
const REPORT_MS = 2_000;

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

    const isLead = () => {
      const room = latest.current.room;
      return !!room && room.lead === room.you;
    };
    // The end of a track, or a track that will not play, moves the queue on.
    const reportEnded = (itemId: string | null) => {
      finished = itemId;
      if (itemId && isLead()) {
        latest.current.send({ type: "report", item_id: itemId, state: "ended" });
      }
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
        onEnded: () => active === provider && reportEnded(loaded),
        onError: () => active === provider && reportEnded(loaded),
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
        { lead, seenSeekSeq },
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
            reportEnded(action.itemId);
            break;
          }
          if (!slot.player) break; // still starting; try again next tick
          if (active !== action.provider) current?.stop();
          active = action.provider;
          show(active);
          slot.player.load(action.trackId, action.startMs);
          loaded = action.itemId;
          finished = null;
          switched = true;
          continue;
        }
        const player = active ? slots.get(active)?.player : null;
        if (!player) break;
        switch (action.kind) {
          case "stop":
            player.stop();
            loaded = null;
            break;
          case "seek":
            player.seek(action.toMs);
            finished = null;
            break;
          case "play":
            player.play();
            break;
          case "pause":
            player.pause();
            break;
        }
      }
      if (now) seenSeekSeq = now.seek_seq;
      // What this speaker is doing, for tests and for anyone inspecting it.
      const stack = holder.current;
      if (stack) {
        stack.dataset.provider = active ?? "";
        stack.dataset.playing = String(state.playing);
        stack.dataset.positionMs = String(Math.round(state.positionMs));
      }

      // Only a player that has started reports, so a track still loading
      // does not pull the clock back to zero.
      const due = Date.now() - lastReport >= REPORT_MS;
      if (
        lead &&
        !switched &&
        state.playing &&
        now &&
        loaded === now.item.id &&
        now.paused_at_ms === null &&
        due
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
