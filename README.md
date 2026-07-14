# Go Job Runner

A concurrent job execution system built in Go using a worker pool pattern with PostgreSQL-backed persistence and real-time job dispatch via `LISTEN/NOTIFY`.

## Architecture

```
                                  ┌─────────────────────────────────────────────────────┐
                                  │                   PostgreSQL                        │
                                  │                                                     │
                                  │  ┌────────────┐  ┌──────────────┐                   │
                                  │  │   jobs      │  │ job_result  │                   │
                                  │  │            ◄├──┤►             │                   │
                                  │  │ id (UUID)   │  │ job_id (FK)  │                   │
                                  │  │ type        │  │ job_payload  │                   │
                                  │  │ status      │  │ result_data  │                   │
                                  │  │ attempt_cnt │  └──────────────┘                   │
                                  │  │ max_retries │                                    │
                                  │  └──────┬─────┘                                    │
                                  │         │                                           │
                                  │         │ ON INSERT trigger                         │
                                  │         ▼                                           │
                                  │  ┌──────────────┐                                   │
                                  │  │NOTIFY new_job│                                   │
                                  │  └──────┬───────┘                                   │
                                  └─────────┼───────────────────────────────────────────┘
                                            │
┌───────────────────────┐                   │
│     HTTP Server       │                   │
│     (Chi Router)      │                   │
│                       │                   │
│  POST /create-job ────┼──► INSERT ────────┘
│  GET  /job/:id/status │                   │
│  GET  /health         │                   │
└───────────────────────┘         ┌─────────▼──────────┐
                                  │  LISTEN new_job    │
                                  │  (dedicated conn)  │
                                  └─────────┬──────────┘
                                            │
                                    ┌───────▼───────┐
                                    │   Channel     │
                                    │  (buffered)   │
                                    └───┬───┬───┬───┘
                                        │   │   │
                              ┌─────────┘   │   └─────────┐
                              ▼             ▼             ▼
                        ┌──────────┐  ┌──────────┐  ┌──────────┐
                        │ Worker 1 │  │ Worker 2 │  │ Worker N │
                        │          │  │          │  │          │
                        │ Fetch    │  │ Fetch    │  │ Fetch    │
                        │ payload  │  │ payload  │  │ payload  │
                        │    │     │  │    │     │  │    │     │
                        │    ▼     │  │    ▼     │  │    ▼     │
                        │ Execute  │  │ Execute  │  │ Execute  │
                        │ job      │  │ job      │  │ job      │
                        │    │     │  │    │     │  │    │     │
                        │    ▼     │  │    ▼     │  │    ▼     │
                        │ Update   │  │ Update   │  │ Update   │
                        │ status   │  │ status   │  │ status   │
                        └──────────┘  └──────────┘  └──────────┘
```

## How It Works

1. A client submits a job via the REST API
2. The service layer inserts the job into PostgreSQL with status `pending`
3. A database trigger fires `NOTIFY new_job` with the job ID and type
4. A dedicated listener goroutine receives the notification and pushes it onto a buffered channel
5. One of N worker goroutines picks up the job, sets status to `processing`, executes it, and marks it `completed` or `failed`
6. The client polls the status endpoint to track progress

## Project Structure

```
cmd/server/main.go        Entry point — loads config, connects DB, starts workers & server
configs/config.go          Environment variable loading (DB_URL, SERVER_PORT, WORKERS, QUEUE_SIZE)
database/
  database.go              PostgreSQL connection pool (pgx) with health checks
  repository.go            Generic repository with type-safe CRUD operations
jobs/
  handler.go               Job dispatcher — routes job types to their handlers
  healthcheck.go           Health check job — concurrent HTTP checks with WaitGroup
migrations/
  create-jobs.sql           Schema: jobs + job_result tables, indexes, NOTIFY trigger
  seed.go                   Migration runner — executes .sql files in order
routes/routes.go           Chi router — POST /create-job, GET /job/:id/status, GET /health
services/
  healthcheck.go            Business logic for job creation
  jobstatus.go              Job status retrieval
utils/utils.go             URL normalization, response reading, error logging
workerpool/worker-pool.go  Worker pool — listener goroutine + N worker goroutines
```

## API

### Create a Job

```bash
curl -X POST http://localhost:5000/create-job \
  -H "Content-Type: application/json" \
  -d '{"job_type": "health-check", "url_list": ["https://google.com", "https://github.com"]}'
```

```json
{ "success": true, "status": "pending", "job_id": "a1b2c3d4-..." }
```

### Check Job Status

```bash
curl http://localhost:5000/job/{job_id}/status
```

```json
{ "success": true, "status": "completed" }
```

### Health Check

```bash
curl http://localhost:5000/health
```

## Concurrency Model

| Mechanism            | Purpose                                                   |
| -------------------- | --------------------------------------------------------- |
| **Goroutines**       | N configurable workers running in parallel                |
| **Buffered channel** | Distributes jobs from the PG listener to workers          |
| **sync.WaitGroup**   | Parallelizes HTTP checks within a single health-check job |
| **pgxpool**          | Connection pooling (max 10, min 2, 1h lifetime)           |
| **PG LISTEN/NOTIFY** | Event-driven dispatch — no polling                        |

## Job Lifecycle

```
pending ──► processing ──► completed
                │
                └──────────► failed (last_error recorded)
```

## Supported Job Types

| Type           | Description                                                                                                                         |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| `health-check` | Performs concurrent HTTP GET requests against a list of URLs and records status codes, response bodies, and success/failure per URL |

New job types can be added by implementing a handler function and registering it in `jobs/handler.go`.

## Setup

```bash
# configure environment
cp .env.example .env
# DB_URL=postgres://user:pass@host:5432/dbname
# SERVER_PORT=5000
# WORKERS=5
# QUEUE_SIZE=100

# run migrations
go run migrations/seed.go

# start the server
go run cmd/server/main.go
```

## Tech Stack

- **Go** — language
- **Chi** — HTTP router
- **pgx** — PostgreSQL driver with connection pooling
- **PostgreSQL** — job persistence + LISTEN/NOTIFY for real-time dispatch

## Monitoring (Prometheus + Grafana)

The service exposes Prometheus metrics at `GET /metrics`:

| Metric | Type | Meaning |
|---|---|---|
| `jobrunner_jobs_processed_total{job_type,status}` | counter | Jobs completed/failed (throughput + errors) |
| `jobrunner_job_duration_seconds{job_type}` | histogram | Job execution latency |
| `jobrunner_queue_depth` / `jobrunner_queue_capacity` | gauge | Dispatch channel saturation |
| `jobrunner_notifications_received_total` | counter | pg_notify events received (input rate) |

The observability stack itself is deployed via GitOps (see the
[go-job-runner-gitops](https://github.com/tejas261/go-job-runner-gitops) repo):

- **kube-prometheus-stack** (Prometheus Operator + Grafana + Alertmanager) is
  declared as an ArgoCD `Application` (`argocd/monitoring-app.yaml`), pinned to
  a chart version and synced with prune + self-heal + server-side apply.
- Prometheus discovers the app through the `prometheus.io/scrape` pod
  annotations set by the Helm chart (annotation-based `kubernetes_sd` scrape
  config in the monitoring app's values).
- The **"Job Runner — Golden Signals"** Grafana dashboard ships with the
  service's own chart as a ConfigMap labeled `grafana_dashboard: "1"`, imported
  automatically by the Grafana sidecar — dashboards as code, versioned next to
  the chart that emits the metrics.

Rationale: instrumentation lives in the app (the code owns its metrics);
scrape config, the monitoring stack, and dashboards are cluster state and live
in the GitOps repo.
