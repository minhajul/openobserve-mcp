# OpenObserve MCP

A working local observability environment where any MCP-compatible client (Claude, Cursor, Gemini, etc.) calls a clean
abstraction layer that talks to OpenObserve.

The client never sees OpenObserve URLs, stream names, SQL dialect, authentication, or HTTP details. Those live behind
the MCP server.

---

## 1. Architecture

| Component              | Path              | Responsibility                                             |
|------------------------|-------------------|------------------------------------------------------------|
| `mcp-server`           | `cmd/mcp-server`  | Speaks MCP over stdio. Exposes typed observability tools.  |
| `seed`                 | `cmd/seed`        | Loads deterministic sample data via OpenObserve ingestion. |
| `internal/openobserve` | client + adapters | The ONLY place OpenObserve URLs, auth, and HTTP live.      |
| `internal/sqlbuilder`  | query generation  | Builds OpenObserve SQL; validates all user input.          |
| `internal/mcp`         | tool registration | Tool schemas, parameter validation, structured logging.    |
| `internal/config`      | typed config      | Env-var parsing + fail-fast validation.                    |

---

## 2. Prerequisites

- Go **1.22+**
- Docker with Compose

---

## 3. Setup

```bash
cp .env.example .env       # then edit if you changed credentials

make up                     # starts OpenObserve, waits for /healthz
make seed                   # loads ~500 logs, 120 metrics, 60 spans
```

The seed utility is **deterministic** — re-running it produces the same shape of data so queries are reproducible.
Records are spread over the hour before `make seed` runs and carry their event time in OpenObserve's indexed
`_timestamp` column, so the tools' default `1h` window sees everything right after seeding. (OpenObserve rejects event
times older than 5h by default, so the data can't be backdated further.)
Re-running appends; to start from a clean slate, run `make clean && make up seed`.

### Cleaning up

```bash
make clean                     # see below for what this removes
```

`make clean` is destructive — it removes:

- the running OpenObserve container (`docker compose down`)
- the OpenObserve Docker image (`public.ecr.aws/zinclabs/openobserve:latest`)
- the OpenObserve data volume (`openobserve-data`) — including any seeded data
- the `bin/` directory (built `mcp-server` and `seed` binaries)
- the Go build and test caches (`go clean -cache -testcache`, machine-wide)
- any stray seed manifests

Missing artifacts are ignored, so it's safe to invoke repeatedly.

---

## 4. Environment variables

| Variable               | Default                 | Purpose                       |
|------------------------|-------------------------|-------------------------------|
| `OPENOBSERVE_URL`      | `http://localhost:5080` | OpenObserve base URL.         |
| `OPENOBSERVE_ORG`      | `default`               | Organization / tenant.        |
| `OPENOBSERVE_USERNAME` | `root@example.com`      | HTTP basic auth username.     |
| `OPENOBSERVE_PASSWORD` | `Complexpass#123`       | HTTP basic auth password.     |
| `OPENOBSERVE_TIMEOUT`  | `30s`                   | HTTP request timeout.         |
| `MCP_LOG_FILE`         | (empty = stderr)        | Optional JSON log file path.  |
| `MCP_LOG_LEVEL`        | `info`                  | Log level: `info` or `debug`. |

---

## 5. Running the MCP server

```bash
make build
make mcp                       # serves MCP over stdio
```

The MCP server is meant to be launched by an MCP client as a subprocess. Configure your client to run `bin/mcp-server`
(or `go run ./cmd/mcp-server`) and point it at the OpenObserve instance you started with `make up`.

---

## 6. MCP tools exposed

| Tool                 | Purpose                                                        |
|----------------------|----------------------------------------------------------------|
| `list_services`      | Services that emitted logs, with counts (discovery).           |
| `list_metrics`       | Metric names with data, with sample counts (discovery).        |
| `search_logs`        | Filtered log search (service, level, status, message, …).      |
| `get_recent_logs`    | Most recent logs, optionally filtered.                         |
| `search_errors`      | Convenience: ERROR-level logs.                                 |
| `query_metrics`      | Aggregation over a metric (`avg`, `sum`, `p50`…`p99`, …).      |
| `get_metric`         | Average value of a metric across a time window.                |
| `search_traces`      | Span search by service/operation/status/duration.              |
| `get_trace`          | All spans for a given `trace_id`, in start order (last 7d).    |
| `get_service_errors` | Error count per service, most first.                           |
| `get_slow_requests`  | Requests above a duration threshold, slowest first.            |
| `get_error_summary`  | Total errors + per-service and per-status breakdown.           |

Each tool has a typed JSON schema; the client only sees these names, descriptions, and typed parameters.

Windowed tools accept `since` (`15m`, `1h`, `24h`, `7d`; a bare number means minutes) or RFC3339 `start_time` /
`end_time`. Timestamps in results are RFC3339 UTC; span `duration` is in microseconds.

---

## 7. How a tool call flows

To answer a question, the MCP server:

1. validates the parameters,
2. builds the appropriate OpenObserve SQL inside `internal/sqlbuilder`,
3. hits the `/_search` endpoint via HTTP basic auth,
4. returns a JSON result that the client uses to compose its answer.

User input never reaches SQL as raw text: values are quoted and escaped, and anything used as an identifier (stream,
`group_by` field, aggregation, level) is checked against an allowlist or an identifier pattern. The generated SQL is
logged at `MCP_LOG_LEVEL=debug` and is never returned to the client.

The MCP layer is the only place that knows about OpenObserve.

---

## 8. Project layout

```text
.
├── cmd/
│   ├── mcp-server/      # MCP server (stdio)
│   └── seed/            # deterministic data loader
├── internal/
│   ├── config/          # typed env-driven config
│   ├── openobserve/     # the ONLY place OpenObserve details live
│   │   ├── client.go    # HTTP client + auth + URL building + schema cache
│   │   ├── search.go    # /_search call + result normalization
│   │   ├── logs.go      # log search/aggregation + ingest
│   │   ├── metrics.go   # metric aggregation + ingest
│   │   └── traces.go    # span search + ingest
│   ├── sqlbuilder/      # pure SQL generation + input validation
│   └── mcp/             # MCP server + tool schemas
├── docker-compose.yml
├── .env.example
├── Makefile
├── go.mod / go.sum
└── README.md
```

---

## 9. Troubleshooting

| Symptom                                                | Cause / fix                                                                    |
|--------------------------------------------------------|--------------------------------------------------------------------------------|
| `openobserve not healthy` warning                      | Container still starting. `make up` polls `/healthz` for up to ~60s.           |
| `missing required configuration: OPENOBSERVE_PASSWORD` | Set the variable in `.env` (or rely on default).                               |
| `401 unauthorized`                                     | Wrong credentials. The default bootstrap user only exists with a fresh volume. |
| MCP server hangs                                       | Check that `OPENOBSERVE_URL` is reachable from the MCP server process.         |
| Empty query results                                    | Seed data is from the hour before `make seed`; widen with `since=24h`.         |
| `docker compose up -d` returns EOF                     | Docker daemon not running.                                                     |

---

## 10. Security considerations

- No credentials live in source. `.env.example` documents defaults, and the real `.env` is git-ignored.
- The MCP server uses HTTP basic auth. In production, replace the default bootstrap user and disable the
  `/api/<org>/<stream>/_json`
  public ingestion (set `ZO_INGEST_ALLOWLIST` etc.).
- Structured logs only carry `tool`, `duration_ms`, and error messages — no secrets.
- The MCP client never sees raw OpenObserve URLs or SQL.
- `MCP_LOG_FILE` is created with `0600` permissions; use it if you want logs kept off stderr.

---

## 11. Acceptance verification

The repository was designed to satisfy every checkbox in section 19 of the project brief. To re-run the full
verification:

```bash
make build
make up                     # OpenObserve + healthcheck
make seed                   # load sample data
./bin/mcp-server            # start MCP server, then connect any MCP client
```

## Author

Made with ❤️ by [Minhajul](https://github.com/minhajul)