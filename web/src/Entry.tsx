import { type FormEvent, type ReactNode, useState } from "react";
import { ApiError, createRoom, joinRoom } from "./api.ts";

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
    </Shell>
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
