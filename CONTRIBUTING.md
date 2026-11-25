# Contributing

Thanks for helping. Small, focused changes are the easiest to review. For
anything larger, open an issue first so we can agree on the approach.

## Before you start

- [AGENTS.md](AGENTS.md) holds the project rules (ownership boundaries, naming,
  product rules, engineering constraints). It applies to people as much as to
  coding agents.
- [docs/code-map.md](docs/code-map.md) tells you which file or package owns
  what. Start there before searching the code.
- Code is the source of truth. If a doc disagrees with the code, fix the doc.

## Setup

The README's [Run it yourself](README.md#run-it-yourself) and
[Development](README.md#development) sections cover the stack, the database
tests and the JavaScript workspace. You need Docker, Go, and Node 26+ with pnpm.
A Factorio account with Space Age is only needed for the render pipeline, the
game data generator and the recovery e2e suite; most of the code and its tests
run without it.

## Checks to run

```sh
go build ./... && go test ./...
pnpm install
pnpm lint:check && pnpm format:check && pnpm test && pnpm build
```

- Formatting and linting use oxfmt and oxlint through the root pnpm scripts.
- `pnpm install` installs the lefthook git hooks: staged files are checked
  (gofmt, go vet, oxfmt, oxlint) before each commit, commit messages must follow
  [Conventional Commits](https://www.conventionalcommits.org) (`feat: ...`,
  `fix(api): ...`), and direct pushes to `master` are refused.
- If you change a route, regenerate the OpenAPI documents and the TypeScript
  types as described in AGENTS.md; tests fail while they are stale.
- If you change sqlc queries, regenerate `internal/db/generated/`.
- If you change a user-visible flow, run the e2e suite (`cd e2e && pnpm test`,
  see [docs/e2e.md](docs/e2e.md)) and update its specs.
- Keep `docs/code-map.md` and `docs/bridge-protocol.md` current when you touch
  what they describe, and list new fixtures in `docs/test-data.md`.

## Pull requests

- AI-assisted contributions are welcome. You own what you submit: review it,
  run the checks below and understand it before opening the pull request.
- One topic per pull request, with tests for new behaviour and bug fixes.
- Explain the why in the description, and mention anything you could not run.
- Do not commit game assets (icon sheets, previews, screenshots of game
  renders you do not own), secrets, or other people's blueprints as fixtures
  without their permission.

## Reporting bugs and ideas

Open an issue with the steps to reproduce, what you expected and what
happened. For security problems, see [SECURITY.md](SECURITY.md) instead.

## License

By contributing you agree that your work is released under the
[MIT License](LICENSE).
