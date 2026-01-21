# Repository Guidelines

## Project Structure & Module Organization
- `cmd/server/main.go` is the entry point for the API server.
- `internal/` holds core packages: `config`, `credential`, `gateway`, `middleware`, `signature`, `vertex`, and shared `pkg` helpers.
- `internal/gateway/manager/views/` contains `.templ` UI templates (generated Go files end with `_templ.go`).
- `data/` stores runtime data (for example `accounts.json` and signatures); avoid committing sensitive values.
- `benchmark.sh` and `benchmark_results/` capture performance profiles and summaries.
- `server` is the built binary; `start.sh` orchestrates build + run.

## Build, Test, and Development Commands
- `./start.sh`: loads `.env`, checks required vars, builds templ + Go, and launches the server on `HOST:PORT`.
- `templ generate internal/gateway/manager/views`: regenerates templ views (installed via `go install github.com/a-h/templ/cmd/templ@latest`).
- `go build -o server ./cmd/server`: builds the backend binary.
- `go test ./...`: runs unit tests across all packages.
- `docker build -t ant2api .`: builds the container image.
- `docker-compose up -d`: runs the published image with env overrides.

## Coding Style & Naming Conventions
- Go code should stay `gofmt`-clean and follow standard Go package naming (lowercase, no underscores).
- Tests live alongside code and use the `*_test.go` suffix.
- Environment variables are uppercase snake case and documented in `.env`.
- Keep gateway-specific code inside `internal/gateway/<provider>/` to avoid cross-provider coupling.

## Testing Guidelines
- Primary framework is Go’s `testing` package; run `go test ./...` before submitting.
- New behavior should include a focused unit test in the same package.
- If adding UI templates, regenerate templ output and ensure tests still pass.

## Commit & Pull Request Guidelines
- Commit messages are short and descriptive; history shows both plain Chinese summaries and type prefixes like `chore:`.
- Prefer imperative, single-scope subjects (example: `chore: ignore accounts.json files`).
- PRs should include: a brief summary, how to run tests, and any `.env` or data format changes.
- Include screenshots for UI changes under `internal/gateway/manager/views/`.

## Security & Configuration Tips
- `WEBUI_PASSWORD` is required; `API_KEY` is optional but recommended for protection.
- Keep secrets out of Git; use `.env` or environment injection in Docker.
- Runtime data in `data/` should be treated as stateful and backed up before migrations.
