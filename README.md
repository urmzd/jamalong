<p align="center">
  <h1 align="center">jamalong</h1>
  <p align="center">
    <strong>Group listening rooms with one shared queue, across music providers.</strong>
    <br /><br />
    <a href="#quick-start">Quick Start</a>
    &middot;
    <a href="https://github.com/urmzd/jamalong/issues">Report Bug</a>
  </p>
</p>

<p align="center">
  <a href="https://github.com/urmzd/jamalong/actions/workflows/ci.yml"><img src="https://github.com/urmzd/jamalong/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License"></a>
</p>

## What it is

One person starts a room. Everyone else joins from a link or a QR code, with no
account and no install. The room has one queue, and the queue is the source of
truth: the server orders every change and every device follows it.

A room has two kinds of member device:

- **Remote**: adds tracks, votes and asks to skip.
- **Speaker**: plays the queue. One speaker is a party. Several speakers, each
  on its own provider account, is listening together from different places.

jamalong never relays audio. Each speaker pulls its own stream from its own
provider.

## Quick Start

Requires Go 1.27 or newer, Node 24 or newer and pnpm 11.

```bash
git clone https://github.com/urmzd/jamalong
cd jamalong
make init
pnpm install
pnpm run build
make build
./bin/jamalong -web web/dist
```

Open http://127.0.0.1:8080, start a room, and tap **Play here** on the device
that should make sound. Share the link or QR code. Anyone who opens it can
paste a YouTube, Spotify or SoundCloud track link into the queue.

Behind HTTPS, add `-secure-cookies`.

## How a room works

| Piece     | Rule                                                                                                          |
| --------- | ------------------------------------------------------------------------------------------------------------- |
| Queue     | Fair by person: one track each per round, votes order tracks within a round                                   |
| Roles     | The host controls everything. Collaborators control playback and the queue. Guests add, vote and vote to skip |
| Join link | A 128-bit secret in the URL fragment. The host can replace it at any time                                     |
| Clock     | The server keeps one clock. The lead speaker corrects it, every other speaker follows it                      |
| Providers | YouTube, Spotify and SoundCloud. Each speaker plays in the provider's embed, signed in to its own account     |

## Development

Run the server and the web app side by side. Vite serves the app on port 5173
and proxies the API to the Go server.

```bash
./bin/jamalong
pnpm run dev
```

| Task         | Go           | Web              |
| ------------ | ------------ | ---------------- |
| Test         | `make test`  | `pnpm test`      |
| Lint         | `make lint`  | `pnpm run lint`  |
| Format       | `make fmt`   | `pnpm run fmt`   |
| Quality gate | `make check` | `pnpm run check` |

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md).

## License

[Apache-2.0](LICENSE)
