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

Requires Go 1.27 or newer.

```bash
git clone https://github.com/urmzd/jamalong
cd jamalong
make init
make check
```

## Development

| Task | Command |
| --- | --- |
| Build | `make build` |
| Test | `make test` |
| Lint | `make lint` |
| Format | `make fmt` |
| Quality gate | `make check` |

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md).

## License

[Apache-2.0](LICENSE)
