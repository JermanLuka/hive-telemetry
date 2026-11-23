# hive-telemetry

IoT readings from hive scales and weather stations, ingested over MQTT, stored in
PostgreSQL, aggregated by background jobs and exported for analytics. Written in Go.

A personal project, inspired by my client work on a commercial hive monitoring
system. The production system is built in .NET; this is my own reimplementation
of the backend in Go.

> **Status:** in progress. Readings flow end to end today: simulator → MQTT →
> ingestor → PostgreSQL → HTTP API. Background jobs, analytics export,
> observability and CI/CD are next.

## Architecture

```
simulated sensors ──MQTT──> Mosquitto ──> ingestor ──> PostgreSQL (OLTP)
                                                           │
            API (OpenAPI, ogen) <── reads ─────────────────┤
            web page (HTML/JS) ──> API                     │
            worker (River): hourly averages,               │
                            nightly Parquet export ────────┘──> DuckDB (OLAP)
```

Every Go service is traced with OpenTelemetry. CI on GitHub Actions lints, tests,
builds a container image and pushes it to GHCR. The same image runs with Docker
Compose on a Linux VM or on Kubernetes.

The full design, with how a reading flows through the system, is in
[docs/architecture.html](docs/architecture.html).

## Run it locally

Needs Go 1.27+ and Docker.

```sh
# Postgres and Mosquitto
docker compose -f deploy/compose.yaml up -d --wait

# Create the tables
go run ./cmd/migrate

# Two demo hives with 24 hours of history
docker compose -f deploy/compose.yaml exec -T postgres psql -U hive -d hive < deploy/seed.sql
# PowerShell: Get-Content deploy/seed.sql | docker compose -f deploy/compose.yaml exec -T postgres psql -U hive -d hive
```

Then start each service in its own terminal:

```sh
go run ./cmd/ingestor    # MQTT → PostgreSQL
go run ./cmd/simulator   # publishes a reading per hive every 5 s (-interval to change)
go run ./cmd/api         # HTTP API on :8080
```

```sh
curl localhost:8080/hives
curl "localhost:8080/hives/hive-1/readings?limit=5"
curl localhost:8080/hives/hive-1/summary
curl localhost:8080/healthz
```

Settings come from environment variables; the defaults match the compose file
(see [.env.example](.env.example)). Every service stops gracefully on Ctrl+C or
SIGTERM.

## API

Defined in [api/openapi.yaml](api/openapi.yaml). The server code in
`internal/gen/api` is generated from it with `go generate ./...`.

| Endpoint | Returns |
|---|---|
| `GET /hives` | all hives |
| `GET /hives/{hiveId}/readings?from&to&limit` | readings in `[from, to)`, oldest first; default last 24 h, 1000 rows |
| `GET /hives/{hiveId}/summary?from&to` | count, min, max, average, first, last and change of weight |
| `GET /healthz` | 200 when the database is reachable, 503 otherwise |

Errors are `{"message": "..."}` with status 400, 404 or 500.

## How readings stay correct

- **At-least-once delivery.** Readings are published with MQTT QoS 1. The
  ingestor uses a persistent session and acks a message only after it is
  stored, so nothing is lost if the ingestor or the database is down.
- **Duplicates are harmless.** The primary key is `(hive_id, observed_at)` and
  inserts use `ON CONFLICT DO NOTHING`, so a redelivered message is ignored.
- **Bad messages are dropped, not retried.** Malformed JSON, out-of-range values
  and unknown hives are logged and acked. Database errors are retried with
  backoff (1 s up to 30 s).

## What would change in production

This is a demo with simulated hives. Shortcuts taken on purpose:

| Now | In production |
|---|---|
| MQTT without login or TLS | client certificates or passwords, TLS |
| One ingestor with a fixed client ID | several ingestors sharing the load via MQTT shared subscriptions (`$share/...`) |
| One `INSERT` per message | batched inserts (multi-row or `COPY`) |
| Mosquitto keeps queued messages in memory only | broker persistence, or a managed broker |
| Invalid messages are logged and dropped | a dead-letter table or topic to inspect them later |
| Config falls back to local development defaults | refuse to start without explicit settings |
| API without authentication or rate limiting | auth, rate limits, CORS |
| `limit` without a next page | cursor pagination on `observed_at` |
| Summary aggregates the whole range in one query | first and last reading via the index; precomputed hourly averages (planned worker) |
| One log line per reading | debug level, plus metrics and traces (planned) |
| No retention | time-based partitioning and a retention policy |
| Health check on the API only | `/healthz` on every service for Kubernetes probes |

## Tests

```sh
go test ./...
```

The store tests run against a real PostgreSQL. They are skipped unless
`TEST_DATABASE_URL` is set; each test migrates a fresh schema and drops it
afterwards.

```sh
TEST_DATABASE_URL=postgres://hive:hive@localhost:5432/hive?sslmode=disable go test ./...
# PowerShell: $env:TEST_DATABASE_URL="postgres://hive:hive@localhost:5432/hive?sslmode=disable"; go test ./...
```

## Project layout

```
api/                 OpenAPI spec
cmd/api              HTTP API
cmd/ingestor         MQTT subscriber that stores readings
cmd/migrate          database migrations (up, down, status)
cmd/simulator        fake hives publishing over MQTT
deploy/              Docker Compose, Mosquitto config, seed data
internal/config      settings from environment variables
internal/gen/api     code generated by ogen (do not edit)
internal/httpapi     API handlers and error mapping
internal/ingest      message validation and storage
internal/sim         simulated hive values
internal/store       all SQL (pgx)
internal/telemetry   MQTT topics and message format
migrations/          SQL migrations (goose), embedded in cmd/migrate
```

## Stack

| Part | Technology |
|---|---|
| Services | Go |
| API | OpenAPI 3, generated with [ogen](https://github.com/ogen-go/ogen) |
| Ingestion | MQTT (Mosquitto, [paho](https://github.com/eclipse/paho.mqtt.golang)) |
| Storage | PostgreSQL ([pgx](https://github.com/jackc/pgx), [goose](https://github.com/pressly/goose)) |
| Background jobs | [River](https://riverqueue.com/) |
| Analytics | Parquet, DuckDB |
| Observability | OpenTelemetry, Jaeger |
| Delivery | GitHub Actions, golangci-lint, Docker, Kubernetes |

## Roadmap

- [x] Project skeleton, PostgreSQL and migrations
- [x] HTTP API (OpenAPI, ogen)
- [x] MQTT ingestion and hive simulator
- [x] Tests
- [ ] Background jobs and analytics export
- [ ] Observability
- [ ] CI/CD
- [ ] Kubernetes manifests and web page

## License

[MIT](LICENSE)
