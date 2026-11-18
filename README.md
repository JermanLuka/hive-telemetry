# hive-telemetry

IoT readings from hive scales and weather stations, ingested over MQTT, stored in
PostgreSQL, aggregated by background jobs and exported for analytics. Written in Go.

A personal project, inspired by my client work on a commercial hive monitoring
system. The production system is built in .NET; this is my own reimplementation
of the backend in Go.

> **Status:** in progress. The design is done; the code is being built step by step.

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

## Stack

| Part | Technology |
|---|---|
| Services | Go |
| API | OpenAPI 3, generated with [ogen](https://github.com/ogen-go/ogen) |
| Ingestion | MQTT (Mosquitto) |
| Storage | PostgreSQL (pgx) |
| Background jobs | [River](https://riverqueue.com/) |
| Analytics | Parquet, DuckDB |
| Observability | OpenTelemetry, Jaeger |
| Delivery | GitHub Actions, golangci-lint, Docker, Kubernetes |

## Roadmap

- [ ] Go basics and project skeleton
- [ ] API and PostgreSQL
- [ ] MQTT ingestion
- [ ] Background jobs and analytics export
- [ ] Observability
- [ ] Tests and CI/CD
- [ ] Kubernetes manifests and web page

## License

[MIT](LICENSE)
