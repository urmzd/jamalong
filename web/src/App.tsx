import { useEffect, useState } from "react";
import { hasSession } from "./api.ts";
import { Home, Join, Notice } from "./Entry.tsx";
import { RoomScreen } from "./RoomScreen.tsx";

function roomIdFromPath(): string | null {
  const match = /^\/r\/([a-z0-9]+)\/?$/.exec(location.pathname);
  return match?.[1] ?? null;
}

// The join secret rides in the URL fragment, which browsers never send to a
// server. Read it once, before anything can navigate.
const secretFromLink = location.hash.slice(1);

export function App() {
  const roomId = roomIdFromPath();
  if (location.pathname === "/") return <Home />;
  if (!roomId) return <Notice title="Page not found" />;
  return <RoomGate roomId={roomId} />;
}

function RoomGate({ roomId }: { roomId: string }) {
  const [state, setState] = useState<"checking" | "in" | "out">("checking");

  useEffect(() => {
    let live = true;
    hasSession(roomId)
      .then((ok) => live && setState(ok ? "in" : "out"))
      .catch(() => live && setState("out"));
    return () => {
      live = false;
    };
  }, [roomId]);

  useEffect(() => {
    // Once inside, take the secret out of the address bar so it is not left
    // on a shared screen or in history.
    if (state === "in" && location.hash) history.replaceState(null, "", location.pathname);
  }, [state]);

  if (state === "checking") return null;
  if (state === "in") return <RoomScreen roomId={roomId} />;
  if (!secretFromLink) {
    return (
      <Notice title="You need an invite" body="Ask the host for the room's link or QR code." />
    );
  }
  return <Join roomId={roomId} secret={secretFromLink} onJoined={() => setState("in")} />;
}
