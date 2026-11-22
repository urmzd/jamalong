export class ApiError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function post(path: string, body: unknown): Promise<Record<string, string>> {
  const res = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "same-origin",
    body: JSON.stringify(body),
  });
  const data = (await res.json().catch(() => ({}))) as Record<string, string>;
  if (!res.ok) {
    throw new ApiError(res.status, data.code ?? "error", data.message ?? "Something went wrong.");
  }
  return data;
}

export async function createRoom(nickname: string): Promise<string> {
  const data = await post("/api/rooms", { nickname });
  return data.room_id ?? "";
}

export async function joinRoom(roomId: string, secret: string, nickname: string): Promise<void> {
  await post(`/api/rooms/${encodeURIComponent(roomId)}/join`, { secret, nickname });
}

/** Whether this browser already holds a session for the room. */
export async function hasSession(roomId: string): Promise<boolean> {
  const res = await fetch(`/api/rooms/${encodeURIComponent(roomId)}/session`, {
    credentials: "same-origin",
  });
  return res.ok;
}

/** A room someone can ask to join. */
export interface OpenRoom {
  id: string;
  host: string;
  listening: number;
  now_playing?: string;
}

export async function listRooms(): Promise<OpenRoom[]> {
  const res = await fetch("/api/rooms", { credentials: "same-origin" });
  if (!res.ok) return [];
  const data = (await res.json().catch(() => ({}))) as { rooms?: OpenRoom[] };
  return data.rooms ?? [];
}

/** Asks the host to let this visitor in. Returns the token to poll with. */
export async function askToJoin(roomId: string, nickname: string): Promise<string> {
  const data = await post(`/api/rooms/${encodeURIComponent(roomId)}/requests`, { nickname });
  return data.request ?? "";
}

export type Answer = "admitted" | "waiting" | "denied" | "expired";

/** Checks whether the host answered. Admitted sets the session cookie. */
export async function checkRequest(roomId: string, request: string): Promise<Answer> {
  try {
    const data = await post(`/api/rooms/${encodeURIComponent(roomId)}/requests/claim`, { request });
    return data.status === "admitted" ? "admitted" : "waiting";
  } catch (err) {
    if (err instanceof ApiError && err.code === "denied") return "denied";
    if (err instanceof ApiError && err.status === 404) return "expired";
    throw err;
  }
}
