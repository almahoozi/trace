# AGENTS

## Fastest reliable commands
- Build binary: `make build` -> `./bin/trace` from `./cmd/t`.
- Run locally with build metadata: `make run -- <trace-id>`.
- Install CLI to `GOBIN`: `make install`.
- Verify changes: `make test` (currently just `go test -v ./...`; no repo lint/typecheck configs or CI workflows found).

## Repo shape that matters
- Canonical CLI entrypoint is `cmd/t/main.go` (Make targets build/run/install this path).
- `main.go` at repo root is a duplicate of `cmd/t/main.go`; keep behavior aligned if touching startup flow.
- Core wiring: `cmd/t/main.go` -> `internal/config` + `internal/secrets` -> `internal/grafana` + `internal/app/fetcher` -> `internal/tui`.
- Domain structs shared across layers live in `internal/domain/types.go`.

## Runtime/config gotchas
- First run auto-creates config at platform config dir (`~/Library/Application Support/trace/config.json` on macOS; see `internal/config/config.go`).
- `trace config` creates/ensures config file then opens editor from `$VISUAL`/`$EDITOR`; fallback is OS opener.
- Config validation requires at least one environment with `name`, `tempo_datasource_uid`, and `loki_datasource_uid`; invalid config fails startup.
- Grafana token resolution order: env var (default `TRACE_GRAFANA_TOKEN`) first, then token file (default `token` next to config).
- Saved token file permissions are strict (`0600`); config file is `0644`.

## Trace fetching behavior (easy to misread)
- `internal/app/fetcher.go` queries all configured environments concurrently; first environment returning a trace wins and cancels others.
- Logs are prefetched in parallel before trace match is known, then optionally re-fetched for exact trace window.
- If trace/log payload parsing fails, raw JSON is dumped to a temp file (`trace-payload-*.json` / `loki-payload-*.json`) for debugging.
- Empty/default Grafana trace URL template triggers auto-generated Explore URL; custom template bypasses that logic.
