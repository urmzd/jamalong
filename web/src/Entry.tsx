import { Users } from "lucide-react";
import { type FormEvent, type ReactNode, useEffect, useState } from "react";
import {
  ApiError,
  askToJoin,
  checkRequest,
  createRoom,
  joinRoom,
  listRooms,
  type OpenRoom,
} from "./api.ts";

const NAME_KEY = "jamalong.nickname";

function savedName(): string {
  try {
    return localStorage.getItem(NAME_KEY) ?? "";
  } catch {
    return "";
  }
}

function saveName(name: string): void {
  try {
    localStorage.setItem(NAME_KEY, name);
  } catch {
    // Private windows may refuse storage. The name is a convenience only.
  }
}

export function Wordmark() {
  return (
    <a className="wordmark" href="/">
      <img src="/icon.svg" alt="" width="28" height="28" />
      jamalong
    </a>
  );
}

function Shell({ children }: { children: ReactNode }) {
  return (
    <main className="entry">
      <Wordmark />
      {children}
    </main>
  );
}

function NameForm(props: { title: string; action: string; submit(name: string): Promise<void> }) {
  const [name, setName] = useState(savedName);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    saveName(name.trim());
    try {
      await props.submit(name.trim());
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Could not reach the server. Try again.");
      setBusy(false);
    }
  }

  return (
    <form className="card entry-card" onSubmit={onSubmit}>
      <h1>{props.title}</h1>
      <label className="field">
        <span>Your name</span>
        <input
          name="nickname"
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={24}
          autoComplete="nickname"
          enterKeyHint="go"
          required
        />
      </label>
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      <button className="btn btn-primary" type="submit" disabled={busy}>
        {props.action}
      </button>
    </form>
  );
}

export function Home() {
  return (
    <Shell>
      <NameForm
        title="One queue. Everyone adds to it."
        action="Start a room"
        submit={async (name) => {
          const id = await createRoom(name);
          location.assign(`/r/${id}`);
        }}
      />
      <OpenRooms />
    </Shell>
  );
}

const LIST_EVERY_MS = 10_000;

/** Rooms with people in them. Joining one asks the host. */
function OpenRooms() {
  const [rooms, setRooms] = useState<OpenRoom[]>([]);
  useEffect(() => {
    let live = true;
    const load = () =>
      listRooms()
        .then((r) => live && setRooms(r))
        .catch(() => {});
    load();
    const timer = setInterval(load, LIST_EVERY_MS);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, []);

  if (rooms.length === 0) return null;
  return (
    <section className="card queue" aria-label="Open rooms">
      <h2>Open rooms</h2>
      <ol>
        {rooms.map((r) => (
          <li key={r.id} className="row">
            <div className="row-text">
              <p className="row-title">{r.host}'s room</p>
              {r.now_playing && <p className="muted small">{r.now_playing}</p>}
            </div>
            <span className="count">
              <Users aria-hidden size={16} />
              {r.listening}
              <span className="sr-only"> here</span>
            </span>
            <a className="btn" href={`/r/${r.id}`}>
              Ask to join
            </a>
          </li>
        ))}
      </ol>
    </section>
  );
}

export function Join(props: { roomId: string; secret: string; onJoined(): void }) {
  return (
    <Shell>
      <NameForm
        title="Join the room"
        action="Join"
        submit={async (name) => {
          await joinRoom(props.roomId, props.secret, name);
          props.onJoined();
        }}
      />
    </Shell>
  );
}

const POLL_MS = 2_000;

function requestKey(roomId: string): string {
  return `jamalong.request.${roomId}`;
}

function readRequest(roomId: string): string {
  try {
    return sessionStorage.getItem(requestKey(roomId)) ?? "";
  } catch {
    return "";
  }
}

function writeRequest(roomId: string, token: string | null): void {
  try {
    if (token) sessionStorage.setItem(requestKey(roomId), token);
    else sessionStorage.removeItem(requestKey(roomId));
  } catch {
    // Without storage, a reload means asking again.
  }
}

/**
 * A visitor with no link asks the host to let them in, then waits. The
 * request survives a reload of this tab.
 */
export function AskToJoin(props: { roomId: string; onJoined(): void }) {
  const { roomId, onJoined } = props;
  const [token, setToken] = useState(() => readRequest(roomId));
  const [answer, setAnswer] = useState<"denied" | "expired" | null>(null);

  useEffect(() => {
    if (!token) return;
    let live = true;
    const poll = async () => {
      try {
        const result = await checkRequest(roomId, token);
        if (!live || result === "waiting") return;
        writeRequest(roomId, null);
        if (result === "admitted") onJoined();
        else setAnswer(result);
      } catch {
        // A network blip; the next poll tries again.
      }
    };
    void poll();
    const timer = setInterval(poll, POLL_MS);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, [roomId, token, onJoined]);

  if (answer === "denied") return <Notice title="The host did not let you in" />;
  if (answer === "expired") {
    return <Notice title="This room has ended" body="Or your request expired. Start your own." />;
  }
  if (token) {
    return (
      <Shell>
        <div className="card entry-card" role="status">
          <h1>Waiting for the host</h1>
          <p className="muted">The room opens here when the host lets you in.</p>
          <a className="btn" href="/" onClick={() => writeRequest(roomId, null)}>
            Cancel
          </a>
        </div>
      </Shell>
    );
  }
  return (
    <Shell>
      <NameForm
        title="Ask to join"
        action="Ask to join"
        submit={async (name) => {
          const t = await askToJoin(roomId, name);
          writeRequest(roomId, t);
          setToken(t);
        }}
      />
    </Shell>
  );
}

export function Notice(props: { title: string; body?: string }) {
  return (
    <Shell>
      <div className="card entry-card">
        <h1>{props.title}</h1>
        {props.body && <p className="muted">{props.body}</p>}
        <a className="btn btn-primary" href="/">
          Start a room
        </a>
      </div>
    </Shell>
  );
}
