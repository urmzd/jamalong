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

| Path | Holds |
| --- | --- |
| `cmd/jamalong` | Server entry point |
| `internal/` | Private Go packages |

## Commands

| Task | Command |
| --- | --- |
| Quality gate | `make check` |
| Test | `make test` |
| Lint | `make lint` |

## Conventions

- Conventional commits (Angular style).
- Domain logic takes time as an argument. Do not call `time.Now` inside it.
- Go errors are wrapped with `%w` and checked with `errors.Is`.
- No em dashes in prose or comments.
