import { useEffect, useRef } from "react";
import { decide } from "./sync.ts";
import type { RoomSession } from "./useRoom.ts";
import { createPlayer, YT_BUFFERING, YT_ENDED, YT_PLAYING, type YTPlayer } from "./youtube.ts";

const TICK_MS = 500;
const REPORT_MS = 2_000;

/**
 * Plays the room's current track on this device and keeps it on the room
 * clock. When this device is the lead speaker it also reports its position,
 * so the clock follows what people actually hear.
 */
export function Speaker({ session }: { session: RoomSession }) {
  const holder = useRef<HTMLDivElement>(null);
  const latest = useRef(session);
  latest.current = session;

  useEffect(() => {
    const mount = document.createElement("div");
    holder.current?.append(mount);

    let player: YTPlayer | null = null;
    let disposed = false;
    let loaded: string | null = null;
    let finished: string | null = null;
    let seenSeekSeq = 0;
    let lastReport = 0;

    const isLead = () => {
      const room = latest.current.room;
      return !!room && room.lead === room.you;
    };
    // The end of a track, or a video that will not play, moves the queue on.
    const reportEnded = () => {
      finished = loaded;
      if (loaded && isLead()) {
        latest.current.send({ type: "report", item_id: loaded, state: "ended" });
      }
    };

    createPlayer(mount, {
      onState: (state) => {
        if (state === YT_ENDED) reportEnded();
      },
      onError: reportEnded,
    })
      .then((p) => {
        if (disposed) p.destroy();
        else player = p;
      })
      .catch(() => {
        latest.current.send({ type: "speaker", on: false });
      });

    const timer = setInterval(() => {
      const { room, serverNow, send } = latest.current;
      if (!player || !room) return;
      const now = room.now;
      if (now && finished === now.item.id && now.seek_seq === seenSeekSeq) return;

      const state = player.getPlayerState();
      const positionMs = player.getCurrentTime() * 1000;
      const lead = room.lead === room.you;
      const actions = decide(
        now,
        serverNow(),
        { itemId: loaded, playing: state === YT_PLAYING || state === YT_BUFFERING, positionMs },
        { lead, seenSeekSeq },
      );
      for (const action of actions) {
        switch (action.kind) {
          case "stop":
            player.stopVideo();
            loaded = null;
            break;
          case "load":
            player.loadVideoById({ videoId: action.videoId, startSeconds: action.startMs / 1000 });
            loaded = action.itemId;
            finished = null;
            break;
          case "seek":
            player.seekTo(action.toMs / 1000, true);
            finished = null;
            break;
          case "play":
            player.playVideo();
            break;
          case "pause":
            player.pauseVideo();
            break;
        }
      }
      if (now) seenSeekSeq = now.seek_seq;

      const due = Date.now() - lastReport >= REPORT_MS;
      if (lead && now && loaded === now.item.id && now.paused_at_ms === null && due) {
        lastReport = Date.now();
        send({
          type: "report",
          item_id: now.item.id,
          state: state === YT_BUFFERING ? "buffering" : "playing",
          position_ms: Math.round(positionMs),
          duration_ms: Math.round(player.getDuration() * 1000),
        });
      }
    }, TICK_MS);

    return () => {
      disposed = true;
      clearInterval(timer);
      player?.destroy();
      mount.remove();
    };
  }, []);

  return <div className="player" ref={holder} />;
}
