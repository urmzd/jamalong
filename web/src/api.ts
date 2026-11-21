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
