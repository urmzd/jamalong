import {
  ArrowDown,
  ArrowUp,
  Ellipsis,
  ListStart,
  Pause,
  Play,
  Plus,
  QrCode as QrIcon,
  SkipForward,
  ThumbsUp,
  Trash2,
  Users,
  UserX,
  Volume2,
  X,
} from "lucide-react";
import { type FormEvent, type ReactNode, type RefObject, useEffect, useRef, useState } from "react";
import { formatTime, positionMs } from "./clock.ts";
import { Notice, Wordmark } from "./Entry.tsx";
import { Invite } from "./Invite.tsx";
import type { Item, Member, Role, Room } from "./protocol.ts";
import { Speaker } from "./Speaker.tsx";
import { type RoomSession, useRoom } from "./useRoom.ts";

const ROLE_LABEL: Record<Role, string> = {
  host: "Host",
  collaborator: "Collaborator",
  guest: "Guest",
};

const CLOSED_COPY = {
  kicked: { title: "You were removed", body: "The host removed you from this room." },
  ended: { title: "This room has ended", body: undefined },
  gone: { title: "This room is gone", body: "It ended or your session expired." },
  too_many_tabs: {
    title: "Too many tabs",
    body: "This room is open in too many tabs on this device. Close one and reload.",
  },
};

export function RoomScreen({ roomId }: { roomId: string }) {
  const session = useRoom(roomId);
  const { room, joinSecret, closed, error, status, send } = session;
  const invite = useRef<HTMLDialogElement>(null);
  const people = useRef<HTMLDialogElement>(null);

  // The server forgets a speaker when its connection drops. After a network
  // blip this device asks again. After a reload it does not, because the
  // browser needs a fresh tap before it will play sound.
  const [wantSpeaker, setWantSpeaker] = useState(false);
  const isSpeaker = room?.members.find((m) => m.id === room.you)?.speaker ?? false;
  useEffect(() => {
    if (status === "open" && wantSpeaker && room && !isSpeaker) send({ type: "speaker", on: true });
  }, [status, wantSpeaker, isSpeaker, room, send]);

  if (closed) return <Notice {...CLOSED_COPY[closed]} />;
  if (!room) return null;

  const me = room.members.find((m) => m.id === room.you);
  if (!me) return null;
  const controls = me.role !== "guest";
  const names = new Map(room.members.map((m) => [m.id, m.nickname]));
  const here = room.members.filter((m) => m.connected).length;
  const inviteProps = joinSecret
    ? { roomId, secret: joinSecret, onRotate: () => send({ type: "rotate_link" }) }
    : null;

  return (
    <div className="room">
      <header className="bar">
        <Wordmark />
        <div className="bar-actions">
          {inviteProps && (
            <button
              className="icon-btn invite-toggle"
              type="button"
              aria-label="Invite people"
              onClick={() => invite.current?.showModal()}
            >
              <QrIcon aria-hidden size={22} />
            </button>
          )}
          <button
            className="btn"
            type="button"
            aria-label={`People, ${here} here`}
            onClick={() => people.current?.showModal()}
          >
            <Users aria-hidden size={18} />
            {here}
          </button>
        </div>
      </header>

      {status === "reconnecting" && (
        <p className="banner" role="status">
          Reconnecting…
        </p>
      )}

      <main className="col">
        {session.requests.map((r) => (
          <section key={r.id} className="card request" aria-label={`${r.nickname} wants to join`}>
            <p className="row-title">{r.nickname} wants to join</p>
            <div className="request-actions">
              <button
                className="btn"
                type="button"
                onClick={() => send({ type: "deny", request_id: r.id })}
              >
                Decline
              </button>
              <button
                className="btn btn-primary"
                type="button"
                onClick={() => send({ type: "admit", request_id: r.id })}
              >
                Let in
              </button>
            </div>
          </section>
        ))}
        <NowPlaying
          wantSpeaker={wantSpeaker}
          session={session}
          room={room}
          me={me}
          names={names}
          onSpeaker={(on) => {
            setWantSpeaker(on);
            send({ type: "speaker", on });
          }}
        />
        <AddBar onAdd={(url) => send({ type: "add_url", url })} />
        <Queue
          items={room.queue}
          you={room.you}
          controls={controls}
          names={names}
          onVote={(id) => send({ type: "vote", item_id: id })}
          onRemove={(id) => send({ type: "remove", item_id: id })}
          onMove={(id, index) => send({ type: "move", item_id: id, index })}
        />
        {inviteProps && (
          <section className="card invite-card" aria-label="Invite">
            <Invite {...inviteProps} />
          </section>
        )}
      </main>

      {inviteProps && (
        <Sheet dialog={invite} title="Invite people">
          <Invite {...inviteProps} />
        </Sheet>
      )}
      <Sheet dialog={people} title="People">
        <People room={room} me={me} session={session} />
      </Sheet>

      <div className="toasts" aria-live="polite">
        {error && <p className="toast">{error}</p>}
      </div>
    </div>
  );
}

function Sheet(props: {
  dialog: RefObject<HTMLDialogElement | null>;
  title: string;
  children: ReactNode;
}) {
  return (
    <dialog
      className="sheet"
      ref={props.dialog}
      aria-label={props.title}
      onClick={(e) => {
        // A click on the backdrop lands on the dialog element itself.
        if (e.target === e.currentTarget) e.currentTarget.close();
      }}
      onKeyDown={() => {}}
    >
      <div className="sheet-body">
        <div className="sheet-head">
          <h2>{props.title}</h2>
          <button
            className="icon-btn"
            type="button"
            aria-label="Close"
            onClick={() => props.dialog.current?.close()}
          >
            <X aria-hidden size={22} />
          </button>
        </div>
        {props.children}
      </div>
    </dialog>
  );
}

/** Re-renders the caller on an interval while active. */
function useTicker(active: boolean, ms = 500): void {
  const [, setTick] = useState(0);
  useEffect(() => {
    if (!active) return;
    const timer = setInterval(() => setTick((n) => n + 1), ms);
    return () => clearInterval(timer);
  }, [active, ms]);
}

function NowPlaying(props: {
  session: RoomSession;
  room: Room;
  me: Member;
  names: Map<string, string>;
  /** This device asked to play, before the server confirmed it. */
  wantSpeaker: boolean;
  onSpeaker(on: boolean): void;
}) {
  const { session, room, me } = props;
  const { send } = session;
  const now = room.now;
  const controls = me.role !== "guest";
  const playing = !!now && now.paused_at_ms === null;
  useTicker(playing);

  const speakerToggle = (
    <button
      className={me.speaker ? "btn btn-on" : "btn"}
      type="button"
      aria-pressed={me.speaker}
      onClick={() => props.onSpeaker(!me.speaker)}
    >
      <Volume2 aria-hidden size={18} />
      {me.speaker ? "Playing here" : "Play here"}
    </button>
  );

  // The player mounts on the tap itself, not after the server confirms, so
  // the provider's player starts while the browser still counts the tap. It
  // stays mounted while this device is a speaker, even between tracks, so
  // the browser keeps its permission to play sound.
  const media =
    me.speaker || props.wantSpeaker ? (
      <div className="media">
        <Speaker session={session} />
      </div>
    ) : now?.item.track.artwork_url ? (
      <div className="media">
        <img className="artwork" src={now.item.track.artwork_url} alt="" />
      </div>
    ) : null;

  if (!now) {
    return (
      <section className="card now" aria-label="Now playing">
        {media}
        <div className="now-text">
          <h1>Nothing is playing</h1>
          <p className="muted">Add a YouTube, Spotify or SoundCloud link to start the queue.</p>
        </div>
        <div className="now-controls">{speakerToggle}</div>
      </section>
    );
  }

  const track = now.item.track;
  const position = positionMs(now, session.serverNow());
  return (
    <section className="card now" aria-label="Now playing">
      {media}
      <div className="now-text">
        <h1>{track.title}</h1>
        <p className="muted">
          {[track.artist, `added by ${props.names.get(now.item.added_by) ?? "someone"}`]
            .filter(Boolean)
            .join(" · ")}
        </p>
      </div>
      {now.waiting && (
        <p className="waiting" role="status">
          {controls
            ? "No device is playing. Tap play to hear it here."
            : "No device is playing. Tap Play here to hear it."}
        </p>
      )}
      <Progress
        key={now.item.id}
        positionMs={position}
        durationMs={track.duration_ms}
        onSeek={controls ? (ms) => send({ type: "seek", position_ms: ms }) : undefined}
      />
      <div className="now-controls">
        {speakerToggle}
        <div className="transport">
          {controls ? (
            <>
              <button
                className="icon-btn icon-btn-primary"
                type="button"
                aria-label={playing ? "Pause" : "Play"}
                onClick={() => {
                  // Nothing can play without a device. This tap is the one a
                  // browser needs before it plays sound, so play here.
                  if (!playing && now.waiting && !me.speaker) props.onSpeaker(true);
                  send({ type: playing ? "pause" : "play" });
                }}
              >
                {playing ? <Pause aria-hidden size={22} /> : <Play aria-hidden size={22} />}
              </button>
              <button
                className="icon-btn"
                type="button"
                aria-label="Skip"
                onClick={() => send({ type: "skip" })}
              >
                <SkipForward aria-hidden size={22} />
              </button>
            </>
          ) : (
            <button
              className={now.skip_voted ? "btn btn-on" : "btn"}
              type="button"
              aria-pressed={now.skip_voted}
              aria-label={`Vote to skip, ${now.skip_votes} of ${now.skip_needed} votes`}
              onClick={() => send({ type: "skip" })}
            >
              <SkipForward aria-hidden size={18} />
              {now.skip_votes}/{now.skip_needed}
            </button>
          )}
        </div>
      </div>
    </section>
  );
}

function Progress(props: {
  positionMs: number;
  durationMs: number;
  onSeek?: (ms: number) => void;
}) {
  // While the thumb is being dragged, show the drag value, not the clock.
  const [drag, setDrag] = useState<number | null>(null);
  const { durationMs, onSeek } = props;
  const position = drag ?? props.positionMs;

  if (durationMs <= 0) {
    return (
      <div className="progress">
        <span className="time">{formatTime(position)}</span>
      </div>
    );
  }
  return (
    <div className="progress">
      <span className="time">{formatTime(position)}</span>
      {onSeek ? (
        <input
          type="range"
          aria-label="Position in track"
          min={0}
          max={durationMs}
          step={1000}
          value={position}
          onChange={(e) => setDrag(Number(e.target.value))}
          onPointerUp={() => commit()}
          onKeyUp={() => commit()}
          onBlur={() => setDrag(null)}
        />
      ) : (
        <progress aria-label="Position in track" max={durationMs} value={position} />
      )}
      <span className="time">{formatTime(durationMs)}</span>
    </div>
  );

  function commit() {
    if (drag !== null) onSeek?.(drag);
    setDrag(null);
  }
}

function AddBar({ onAdd }: { onAdd(url: string): void }) {
  const [url, setUrl] = useState("");
  function submit(event: FormEvent) {
    event.preventDefault();
    if (!url.trim()) return;
    onAdd(url.trim());
    setUrl("");
  }
  return (
    <form className="add" onSubmit={submit}>
      <input
        type="url"
        name="link"
        inputMode="url"
        aria-label="Track link"
        placeholder="Paste a track link"
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        enterKeyHint="send"
      />
      <button className="icon-btn icon-btn-primary" type="submit" aria-label="Add to queue">
        <Plus aria-hidden size={22} />
      </button>
    </form>
  );
}

function Queue(props: {
  items: Item[];
  you: string;
  controls: boolean;
  names: Map<string, string>;
  onVote(id: string): void;
  onRemove(id: string): void;
  onMove(id: string, index: number): void;
}) {
  const menu = useRef<HTMLDialogElement>(null);
  const [picked, setPicked] = useState<string | null>(null);
  const index = props.items.findIndex((item) => item.id === picked);
  const item = index >= 0 ? props.items[index] : undefined;
  const last = props.items.length - 1;
  // Each action closes the sheet, then sends.
  const act = (run: () => void) => () => {
    menu.current?.close();
    run();
  };

  return (
    <section className="card queue" aria-label="Up next">
      <h2>Up next</h2>
      {props.items.length === 0 ? (
        <p className="muted">The queue is empty.</p>
      ) : (
        <ol>
          {props.items.map((item) => {
            const mine = item.added_by === props.you;
            return (
              <li key={item.id} className="row">
                {item.track.artwork_url ? (
                  <img className="thumb" src={item.track.artwork_url} alt="" loading="lazy" />
                ) : (
                  <div className="thumb" />
                )}
                <div className="row-text">
                  <p className="row-title">{item.track.title}</p>
                  <p className="muted small">
                    {[item.track.artist, props.names.get(item.added_by)]
                      .filter(Boolean)
                      .join(" · ")}
                  </p>
                </div>
                {!mine && (
                  <button
                    className={item.voted ? "btn btn-on" : "btn"}
                    type="button"
                    aria-pressed={item.voted}
                    aria-label={`Vote for ${item.track.title}, ${item.votes} votes`}
                    onClick={() => props.onVote(item.id)}
                  >
                    <ThumbsUp aria-hidden size={18} />
                    {item.votes}
                  </button>
                )}
                {props.controls ? (
                  <button
                    className="icon-btn"
                    type="button"
                    aria-label={`Actions for ${item.track.title}`}
                    onClick={() => {
                      setPicked(item.id);
                      menu.current?.showModal();
                    }}
                  >
                    <Ellipsis aria-hidden size={20} />
                  </button>
                ) : (
                  mine && (
                    <button
                      className="icon-btn"
                      type="button"
                      aria-label={`Remove ${item.track.title}`}
                      onClick={() => props.onRemove(item.id)}
                    >
                      <X aria-hidden size={20} />
                    </button>
                  )
                )}
              </li>
            );
          })}
        </ol>
      )}
      {props.controls && (
        <Sheet dialog={menu} title={item?.track.title ?? "Track"}>
          {item && (
            <div className="actions">
              <button
                className="btn"
                type="button"
                disabled={index === 0}
                onClick={act(() => props.onMove(item.id, 0))}
              >
                <ListStart aria-hidden size={18} />
                Play next
              </button>
              <button
                className="btn"
                type="button"
                disabled={index === 0}
                onClick={act(() => props.onMove(item.id, index - 1))}
              >
                <ArrowUp aria-hidden size={18} />
                Move up
              </button>
              <button
                className="btn"
                type="button"
                disabled={index === last}
                onClick={act(() => props.onMove(item.id, index + 1))}
              >
                <ArrowDown aria-hidden size={18} />
                Move down
              </button>
              <button
                className="btn btn-danger"
                type="button"
                onClick={act(() => props.onRemove(item.id))}
              >
                <Trash2 aria-hidden size={18} />
                Remove
              </button>
            </div>
          )}
        </Sheet>
      )}
    </section>
  );
}

function People(props: { room: Room; me: Member; session: RoomSession }) {
  const { room, me } = props;
  const { send } = props.session;
  const [confirmEnd, setConfirmEnd] = useState(false);
  const host = me.role === "host";
  // The host sets every other member's role, so that row shows a picker in
  // place of the role text.
  const canManage = (m: Member) => host && m.id !== me.id;

  return (
    <>
      <ul className="people">
        {room.members.map((m) => (
          <li key={m.id} className="row">
            <span className={m.connected ? "dot dot-on" : "dot"} aria-hidden />
            <div className="row-text">
              <p className="row-title">
                {m.nickname}
                {m.id === me.id && <span className="muted"> (you)</span>}
              </p>
              <p className="muted small">
                {[
                  !canManage(m) && ROLE_LABEL[m.role],
                  m.speaker && "speaker",
                  !m.connected && "away",
                ]
                  .filter(Boolean)
                  .join(" · ")}
              </p>
            </div>
            {canManage(m) && (
              <>
                <select
                  aria-label={`Role for ${m.nickname}`}
                  value={m.role}
                  onChange={(e) =>
                    send({ type: "set_role", member_id: m.id, role: e.target.value as Role })
                  }
                >
                  <option value="guest">Guest</option>
                  <option value="collaborator">Collaborator</option>
                </select>
                <button
                  className="icon-btn"
                  type="button"
                  aria-label={`Remove ${m.nickname} from the room`}
                  onClick={() => send({ type: "kick", member_id: m.id })}
                >
                  <UserX aria-hidden size={20} />
                </button>
              </>
            )}
          </li>
        ))}
      </ul>
      {host && (
        <div className="sheet-foot">
          {confirmEnd ? (
            <>
              <button className="btn" type="button" onClick={() => setConfirmEnd(false)}>
                Keep going
              </button>
              <button
                className="btn btn-danger"
                type="button"
                onClick={() => send({ type: "end" })}
              >
                End for everyone
              </button>
            </>
          ) : (
            <button className="btn btn-danger" type="button" onClick={() => setConfirmEnd(true)}>
              End room
            </button>
          )}
        </div>
      )}
    </>
  );
}
