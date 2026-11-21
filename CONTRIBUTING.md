# Contributing

## Prerequisites

- Go 1.27 or newer
- [golangci-lint](https://golangci-lint.run) v2
- `make`
- Node 24 or newer and pnpm 11, for the web app

## Getting Started

```bash
git clone https://github.com/urmzd/jamalong
cd jamalong
make init
pnpm install
```

## Development

```bash
make check   # format, lint and test
make test    # tests only
make fmt     # format
pnpm run check   # the same gate for the web app
```

## Commit Convention

Use [Conventional Commits](https://www.conventionalcommits.org) in the Angular
style: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`.

## Pull Requests

1. Create a branch from `main`.
2. Keep the change focused and add tests for it.
3. Run `make check` and `pnpm run check`.
4. Open a pull request and fill in the template.

## Code Style

`gofmt` formats the code and `golangci-lint` enforces the rest. Keep domain
logic free of I/O so it stays easy to test.
