// Wire types. They mirror internal/room/view.go and internal/server/ws.go.

export type Role = "host" | "collaborator" | "guest";

export interface Track {
  provider: string;
  provider_id: string;
  title: string;
  artist: string;
  duration_ms: number;
  artwork_url?: string;
}

export interface Member {
  id: string;
  nickname: string;
  role: Role;
  speaker: boolean;
  connected: boolean;
}

export interface Item {
  id: string;
  track: Track;
  added_by: string;
  votes: number;
  voted: boolean;
}

export interface Now {
  item: Item;
  started_at_ms: number;
  paused_at_ms: number | null;
  waiting: boolean;
  seek_seq: number;
  skip_votes: number;
  skip_needed: number;
  skip_voted: boolean;
}

export interface Room {
  id: string;
  version: number;
  you: string;
  lead?: string;
  members: Member[];
  queue: Item[];
  now: Now | null;
}

export type Command =
  | { type: "add_url"; url: string }
  | { type: "remove" | "vote"; item_id: string }
  | { type: "skip" | "play" | "pause" | "rotate_link" | "end" }
  | { type: "seek"; position_ms: number }
  | { type: "speaker"; on: boolean }
  | { type: "set_role"; member_id: string; role: Role }
  | { type: "kick"; member_id: string }
  | {
      type: "report";
      item_id: string;
      state: "playing" | "buffering" | "ended";
      position_ms?: number;
      duration_ms?: number;
      /** Why an ended report was sent. The server only logs it. */
      reason?: string;
    }
  | { type: "ping"; t0: number };

export type ServerMessage =
  | { type: "state"; room: Room; join_secret?: string; server_ms: number }
  | { type: "error"; code: string; message: string; ref?: string }
  | { type: "pong"; t0: number; server_ms: number };
