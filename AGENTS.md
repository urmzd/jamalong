# AGENTS.md

Context for AI coding agents working in this repository.

## What this is

jamalong is a group listening service: a Go server that owns room state, and a
TypeScript PWA that is both the remote and the speaker.

## Invariants

Keep these true in every change.

- **The queue is the source of truth.** Clients send commands. The server
  orders them, bumps the room version and broadcasts the new state. No client
  holds queue state the server did not send.
- **One room model.** A room has a queue, a clock and N speakers. A single
  speaker is N = 1, not a separate mode.
- **No audio relay.** Each speaker plays from its own provider account.
- **Provider tokens stay on the owner's device.** The server never stores them.
- **Join secrets are unguessable.** 128 bits from `crypto/rand`, compared in
  constant time, carried in the URL fragment.
- **Every command is authorized on the server** against the sender's role.

## Layout

| Path                 | Holds                                                                       |
| -------------------- | --------------------------------------------------------------------------- |
| `cmd/jamalong`       | Server entry point and flags                                                |
| `internal/room`      | Room state: members, fair queue, playback clock. No I/O, no wall clock      |
| `internal/server`    | HTTP API, WebSocket, sessions, join secrets, rate limits                    |
| `internal/provider`  | Turns a pasted link into a track: YouTube, Spotify, SoundCloud              |
| `web/src`            | The PWA. `sync.ts` and `clock.ts` are pure and unit tested                  |
| `web/src/styles.css` | Design tokens: color, type, spacing, radius. Change tokens, not rule values |
| `showcase/`          | Screenshots of the real app, used in pull requests                          |

## Commands

| Task         | Go           | Web              |
| ------------ | ------------ | ---------------- |
| Quality gate | `make check` | `pnpm run check` |
| Test         | `make test`  | `pnpm test`      |
| Lint         | `make lint`  | `pnpm run lint`  |
| Build        | `make build` | `pnpm run build` |

## Web

- One column at every width. Design the phone screen first.
- The wire types in `web/src/protocol.ts` mirror `internal/room/view.go` and
  `internal/server/ws.go`. Change them together.
- The content security policy in `internal/server/server.go` allows this
  origin plus the YouTube, Spotify and SoundCloud players. A new external
  host needs an entry there. Do not add `'unsafe-eval'`.
- Each provider's player sits behind the `Player` interface in
  `web/src/player.ts`. Playback stays inside the provider's embed, so the
  listener's provider login stays in their browser.
- A speaker must be turned on by a tap. Browsers refuse sound otherwise, so
  the server forgets a speaker when its connection drops.

## Conventions

- Conventional commits (Angular style).
- Domain logic takes time as an argument. Do not call `time.Now` inside it.
- Go errors are wrapped with `%w` and checked with `errors.Is`.
- No em dashes in prose or comments.
