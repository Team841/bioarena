# Repository Guidelines

## Project Structure & Module Organization
`main.go` is the entry point for the Go web server. Core domains live in top-level packages such as `field/`, `game/`, `network/`, `partner/`, `playoff/`, `tournament/`, and `websocket/`. Web UI assets are in `web/`, `static/`, and `templates/`. Pre-generated schedules are in `schedules/`. BoltDB data is stored in `db/` (and test fixtures in `*_test.db` files at the repo root).

## Build, Test, and Development Commands
Use Go 1.23+ (see `go.mod`).
1. `go build`
   Builds the `cheesy-arena` binary in the repo root.
1. `./cheesy-arena`
   Runs the server; open `http://localhost:8080` in a browser.
1. `go test ./...`
   Runs all Go tests across packages.

## Coding Style & Naming Conventions
Follow standard Go style: tabs for indentation, exported names in `CamelCase`, unexported in `camelCase`. Format code with `gofmt` before submitting changes. Keep package names short and domain-focused (matching existing directories like `field`, `game`, `partner`).

## Testing Guidelines

### Go tests
Tests are Go `*_test.go` files co-located with packages (for example `field/`, `game/`, `partner/`, `playoff/`). Use `go test ./...` for the full suite and `go test ./field -run TestName` to target specific areas. When adding new behavior, add or update tests in the same package and prefer table-driven tests for coverage.

### JavaScript tests
Client-side behaviour (DOM state, WebSocket message handlers, input sync logic) **cannot be caught by Go tests** and must be covered by Jest. Run with:

```bash
npm run test:js
```

Tests live in `static/js/__tests__/` and use `jest-environment-jsdom` with jQuery available as a global. When adding or changing any JavaScript feature:

1. Add a `module.exports` guard at the bottom of the `.js` file so Jest can import the functions:
   ```javascript
   if (typeof module !== "undefined") {
     module.exports = { myFunction };
   }
   ```
2. Create or update the corresponding test file in `static/js/__tests__/`.
3. Cover the key state-transition scenarios (e.g. what the handler does when the server says the slot is empty vs. occupied, whether user-typed values survive a status push, etc.).

Both `go test ./...` and `npm run test:js` must pass before committing.

## Commit & Pull Request Guidelines
Commit messages in this repo are short, imperative sentences (for example “Fix driver station TCP reads”) and often include an issue/PR number in parentheses (for example “... (#258)”). Keep to that style.

PRs should include:
1. A clear summary of the change.
1. Test notes (exact commands run, for example `go test ./...`).
1. UI screenshots when changing pages in `web/`, `static/`, or `templates/`.

## Configuration & Ops Notes
Cheesy Arena is designed to run as a local web server and uses BoltDB for data. For field networking and hardware integrations, see the project README and relevant `field/` or `plc/` code before making behavioral changes.
