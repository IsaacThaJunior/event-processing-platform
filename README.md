# Task Queue System (Go + Redis + PostgreSQL)

A resilient background task processing system built in Go. Tasks are submitted via HTTP, queued in Redis by priority, processed by a worker pool with retries, scheduling, chaining, and cancellation. File-producing tasks (image resizing, report generation) upload their output to MinIO and return a presigned download URL.

---

## Architecture

```
HTTP Client
    │
    ▼
┌─────────────────────────────────────────┐
│              HTTP API (:8080)           │
│  POST   /tasks                          │
│  DELETE /tasks/:id                      │
│  GET    /tasks/:id/result               │
│  GET    /api/admin/*                    │
└──────────────┬──────────────────────────┘
               │ idempotency check
               │ save event (postgres)
               │ enqueue
               ▼
┌─────────────────────────────────────────┐
│            Redis Priority Queues        │
│   events_queue:high                     │
│   events_queue:medium                   │
│   events_queue:low                      │
│   events_queue:scheduled  (sorted set)  │
│   events_queue:dlq        (dead letter) │
└──────────────┬──────────────────────────┘
               │ BRPOP (blocking dequeue)
               ▼
┌─────────────────────────────────────────┐
│             Worker Pool (N workers)     │
│  ┌─────────────────────────────────┐    │
│  │  fetch event from postgres      │    │
│  │  check cancelled → skip         │    │
│  │  execute task handler           │    │
│  │  upload output → MinIO          │    │
│  │  save result key → postgres     │    │
│  │  on success → enqueue next task │    │
│  │  on failure → retry w/ backoff  │    │
│  │  max retries → DLQ              │    │
│  └─────────────────────────────────┘    │
└──────────┬───────────────┬──────────────┘
           │               │
           ▼               ▼
┌──────────────────┐  ┌────────────────────┐
│   PostgreSQL     │  │   MinIO (:9000)    │
│  events          │  │  images/resized/   │
│  delivery_logs   │  │  reports/          │
│  idempotency_keys│  └────────────────────┘
└──────────────────┘
```

---

## Features

| Feature | Details |
|---|---|
| Priority queues | `high`, `medium`, `low` — workers drain high before low |
| Scheduled tasks | `execute_at` defers a task to a future time |
| Task chaining | `next` field runs tasks sequentially after each succeeds |
| Task cancellation | `DELETE /tasks/:id` cancels any pending task |
| Idempotency | Duplicate requests with the same type/payload/priority are rejected |
| Retry + backoff | Up to 5 attempts: 1s → 2s → 4s → 8s → 16s |
| Dead letter queue | Tasks exceeding max retries moved to DLQ |
| Delivery logs | Every attempt (success, retry, failure) logged to Postgres |
| File storage | File-output tasks upload to MinIO; result retrieved via presigned URL |
| SSRF protection | Image/URL downloads go through a guard that blocks private IP ranges |
| Typed task results | Results stored as `{"kind":"file",...}` or `{"kind":"delivery",...}` |
| Structured logging | Single log line per task with step-level event trail |
| Distributed tracing | OTel spans propagated from HTTP handler through worker |
| Prometheus metrics | Tasks processed, failed, retried, duration — exposed at `/metrics` |
| Graceful shutdown | SIGINT/SIGTERM drains in-flight requests and running tasks before exit |

---

## Task Types

### `resize_image`

Downloads an image from a URL, resizes it, and uploads the result to MinIO.

| Field | Required | Values | Default |
|---|---|---|---|
| `image_url` | Yes | Any public HTTPS URL | — |
| `width` | Yes | `> 0` | — |
| `height` | Yes | `> 0` | — |
| `mode` | No | `fit`, `fill`, `stretch` | `fit` |
| `output_format` | No | `jpeg`, `png` | `jpeg` |
| `quality` | No | `1–100` (JPEG only) | `85` |

**Resize modes:**
- `fit` — scales to fit within bounds, preserves aspect ratio
- `fill` — crops to exact dimensions from center
- `stretch` — distorts to exact dimensions

### `scrape_url`

Fetches a URL, parses the HTML, and stores a structured JSON snapshot (title, headings, paragraphs, links) in MinIO under `scraped/{task_id}.json`. The result is accessible as a presigned download URL.

Can be submitted standalone or with `next.type=generate_report` to automatically produce a CSV report from the scraped content.

| Field | Required |
|---|---|
| `url` | Yes — any public HTTPS URL |

### `generate_report`

**Cannot be submitted directly.** Must be chained as the `next` step after `scrape_url`. The system automatically injects the scraped data key into its payload after scraping completes.

Reads the scraped JSON from MinIO, formats the content as a CSV (URL, title, headings, paragraphs, links), and uploads it to MinIO under `reports/{task_id}.csv`. The result is accessible as a presigned download URL.

---

## API

### Submit a task

```
POST /tasks
```

```json
{
  "type": "resize_image",
  "priority": "high",
  "payload": {
    "image_url": "https://picsum.photos/1200/800",
    "width": 400,
    "height": 300,
    "mode": "fit",
    "output_format": "jpeg",
    "quality": 85
  }
}
```

Scrape a URL and generate a report from it:

```json
{
  "type": "scrape_url",
  "priority": "medium",
  "payload": { "url": "https://example.com" },
  "next": {
    "type": "generate_report",
    "priority": "low"
  }
}
```

Scrape only (no report):

```json
{
  "type": "scrape_url",
  "priority": "medium",
  "payload": { "url": "https://example.com" }
}
```

With scheduling:

```json
{
  "type": "scrape_url",
  "priority": "medium",
  "payload": { "url": "https://example.com" },
  "execute_at": "2026-06-01T09:00:00Z",
  "next": {
    "type": "generate_report",
    "priority": "low"
  }
}
```

**Chaining** — `next` is recursive. Tasks in the chain share the same `parent_id` and run sequentially. A failure stops the chain at that step.

**Responses:**

| Status | Meaning |
|---|---|
| `200` | Task accepted and queued, returns `event_id` |
| `400` | Invalid payload |
| `409` | Duplicate (idempotency collision) |
| `500` | Internal error |

---

### Cancel a task

```
DELETE /tasks/:id
```

Only `pending` tasks can be cancelled. Returns `409` for any other status.

---

### Get task result

```
GET /tasks/:id/result
```

Call this after the task status is `processed`. Returns different shapes depending on the task type.

**File-output tasks** (e.g. `resize_image`, `generate_report`):

```json
{
  "event_id": "abc-123",
  "kind": "file",
  "result_url": "http://localhost:9000/images/resized/abc-123.jpeg?X-Amz-...",
  "expires_in": "24h"
}
```

**Delivery tasks** (e.g. `send_email`):

```json
{
  "event_id": "abc-123",
  "kind": "delivery",
  "recipient": "user@example.com",
  "message_id": "msg_xyz"
}
```

**Not yet processed** (returns `202`):

```json
{
  "event_id": "abc-123",
  "status": "pending",
  "message": "task not yet processed"
}
```

---

### Admin API

All endpoints under `/api/admin/`.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/admin/dashboard/stats` | Task counts by status, queue depths, worker health |
| `GET` | `/api/admin/tasks` | Paginated task list (`?page`, `?page_size`, `?status`, `?type`, `?priority`) |
| `GET` | `/api/admin/tasks/:id` | Single task detail |
| `GET` | `/api/admin/tasks/:id/children` | Tasks chained after this task (e.g. the `generate_report` that ran after a `scrape_url`) |
| `GET` | `/api/admin/tasks/:id/retries` | Retry history for a task |
| `POST` | `/api/admin/tasks/:id/retry` | Re-enqueue a failed or cancelled task |
| `POST` | `/api/admin/tasks/:id/requeue` | Re-enqueue a pending task orphaned from Redis |
| `GET` | `/api/admin/dlq` | All tasks in the dead letter queue |
| `POST` | `/api/admin/dlq/:id/retry` | Move a DLQ task back to the active queue |
| `DELETE` | `/api/admin/dlq/:id` | Remove a task from the DLQ |
| `GET` | `/api/admin/queue/depth` | Current depth of each priority queue |
| `GET` | `/api/admin/workers/health` | Active/idle workers, total processed/failed, uptime |

---

## Polling pattern (frontend)

**Single task (e.g. `resize_image`):**
```
POST /tasks  →  store event_id

poll GET /api/admin/tasks/:id every 2s
  → when status === "processed":
GET /tasks/:id/result  →  show download button with result_url
```

**Chained tasks (e.g. `scrape_url` → `generate_report`):**
```
POST /tasks (scrape_url + next: generate_report)  →  store scrape_event_id

poll GET /api/admin/tasks/:scrape_event_id every 2s
  → when scrape status === "processed" (raw JSON available if needed):

GET /api/admin/tasks/:scrape_event_id/children
  → find the generate_report task  →  store report_event_id

poll GET /api/admin/tasks/:report_event_id every 2s
  → when report status === "processed":

GET /tasks/:report_event_id/result  →  show CSV download button with result_url
```

The `result_url` is a presigned MinIO link valid for 24 hours. Fetch it fresh if the user needs it after expiry.

> **Note on `result_url` not resolving:** set `MINIO_PUBLIC_ENDPOINT=localhost:9000` in your `.env`.
> If this is missing, presigned URLs will contain the internal Docker hostname `minio:9000` which
> the browser cannot reach. See `.env.example`.

---

## Project Structure

```
.
├── cmd/
│   ├── main.go              # wires everything, graceful shutdown
│   └── server.go            # chi router, middleware, routes
├── internal/
│   ├── handler/
│   │   ├── task_handler.go  # create, cancel, get result
│   │   ├── admin_handler.go # admin endpoints
│   │   └── task_sanitizer.go
│   ├── worker/
│   │   ├── pool.go          # worker pool, retry, task routing
│   │   └── ssrf.go          # SSRF guard for URL downloads
│   ├── storage/
│   │   └── minio.go         # MinIO client, upload, presigned URLs
│   ├── repository/
│   │   ├── event_repository.go
│   │   ├── admin_repository.go
│   │   └── redis_queue.go
│   ├── service/
│   │   ├── idempotency.go
│   │   └── task_validator.go
│   ├── domain/
│   │   ├── queue.go         # Queue interface
│   │   └── worker.go        # WorkerHealthProvider interface
│   ├── middleware/
│   │   ├── request_logger.go  # structured per-request logging
│   │   └── trace.go           # OTel trace ID injection
│   ├── metrics/
│   │   └── metrics.go
│   ├── telemetry/
│   │   └── tracer.go
│   ├── sender/
│   │   └── response.go
│   └── database/            # sqlc-generated (do not edit)
├── sql/
│   ├── schema/              # goose migrations
│   └── queries/             # sqlc query definitions
├── docker-compose.yml
├── Dockerfile
└── go.mod
```

---

## Observability Stack

All services start with `docker compose up`.

| Service | URL | Purpose |
|---|---|---|
| App | `localhost:8080` | HTTP API |
| MinIO API | `localhost:9000` | S3-compatible object storage |
| MinIO Console | `localhost:9001` | Browse buckets and uploaded files |
| Prometheus | `localhost:9091` | Metrics scraping |
| Grafana | `localhost:3000` | Dashboards (Prometheus + Loki + Tempo) |
| Loki | `localhost:3100` | Log aggregation |
| Tempo | `localhost:3200` | Distributed trace storage |

Logs from the app are written to `logs/tasks.log` (JSON) and scraped by Promtail into Loki. OTel traces are exported to the collector and forwarded to Tempo.

---

## How to Run

**1. Copy env and start everything**

```bash
cp .env.example .env   # fill in values
docker compose up --build
```

**2. Run migrations**

```bash
goose -dir ./sql/schema postgres "$DB_URL" up
```

**3. Regenerate SQLC** (only after editing `sql/queries/`)

```bash
sqlc generate
```

**4. Submit an image resize task**

```bash
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "type": "resize_image",
    "priority": "high",
    "payload": {
      "image_url": "https://picsum.photos/1200/800",
      "width": 400,
      "height": 300,
      "mode": "fit",
      "output_format": "jpeg",
      "quality": 85
    }
  }'
```

**5. Get the result**

```bash
# Poll until status is processed
curl http://localhost:8080/api/admin/tasks/{event_id}

# Then fetch the presigned download URL
curl http://localhost:8080/tasks/{event_id}/result
```

---

## Environment Variables

```env
# Postgres
DB_URL=postgres://postgres:postgres@postgres:5432/events?sslmode=disable

# Redis
REDIS_HOST=redis
REDIS_PORT=6379

# MinIO
MINIO_ENDPOINT=minio:9000           # internal Docker hostname (server → MinIO)
MINIO_PUBLIC_ENDPOINT=localhost:9000 # browser-accessible hostname (in presigned URLs)
MINIO_ACCESS_KEY=minioadmin
MINIO_SECRET_KEY=minioadmin
MINIO_BUCKET=task-files              # create this bucket in MinIO console (localhost:9001)
MINIO_USE_SSL=false

# OTel
OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4318

# Worker pool (optional — defaults to 3)
# DB MaxConns is set automatically to WORKER_COUNT * 3
WORKER_COUNT=3
```

`MINIO_ENDPOINT` is what the Go server uses to connect to MinIO inside Docker. `MINIO_PUBLIC_ENDPOINT` is what appears in presigned URLs returned to browsers — set this to your public domain in production.

---

## Retry Strategy

```
attempt:  1    2    3    4     5
delay:    1s → 2s → 4s → 8s → 16s
```

After 5 failed attempts the task is moved to the DLQ and its status is set to `failed`. DLQ tasks can be retried or deleted via the admin API.

---

## Design Decisions

| Decision | Why | Trade-off |
|---|---|---|
| Redis lists + sorted set | Simple priority queue with scheduled task support | No built-in durability; at-least-once delivery |
| Idempotency keys in Postgres | Dedup survives restarts | Extra DB read on every request |
| Worker pool (fixed size) | Controlled concurrency, predictable DB connection load | Requires tuning for throughput |
| Task chaining via `next` | Sequential pipelines in a single request | Chain stops on first failure |
| `generate_report` blocked as root task | Report only makes sense over scraped content; enforced at API + backend | Users must submit via `scrape_url` |
| `executeTask` returns extra payload | Allows tasks to forward data to the next step (e.g. `scraped_key`) without a DB round-trip | Adds a small complexity to the worker loop |
| `parent_id` = root task ID | All chain members traceable to origin in O(1) | Slightly denormalized |
| Exponential backoff | Protects downstream on transient failures | Slower recovery at high retry counts |
| SQLC | Compile-time SQL validation, no ORM overhead | Must regenerate after query changes |
| MinIO for file output | S3-compatible, runs locally in Docker, same API in prod | Adds a service dependency |
| Typed result JSON | `{"kind":"file",...}` lets one endpoint serve all task types | Workers must set `kind` correctly |
| SSRF guard via custom dialer | Blocks private IPs at dial time, prevents DNS rebinding | Adds latency for URL downloads |
| `MINIO_PUBLIC_ENDPOINT` | Decouples internal hostname from browser-facing URLs | Requires two env vars instead of one |
| Graceful shutdown with 10s drain | In-flight requests and running tasks finish before process exits | Longer deploy cycle; use shorter timeout if needed |

---

## Production Checklist

- [ ] Add authentication on `/api/admin/*` (currently open)
- [x] Run goose migrations automatically on startup — `goose.Up` runs in `cmd/main.go` before the pool is created; app aborts if any migration fails
- [ ] Set `MINIO_PUBLIC_ENDPOINT` to your public storage domain
- [ ] Use Redis Streams or a dedicated queue (asynq, river) for stronger delivery guarantees
- [x] Tune worker pool size against DB connection pool limits — set `WORKER_COUNT` env var (default `3`); pool `MaxConns` is automatically set to `WORKER_COUNT * 3`
- [x] Monitor the DLQ — `dlqMonitor` goroutine polls every 30s, updates `queue_depth_current{queue="dlq"}` Prometheus gauge, and logs a `WARN` when DLQ is non-empty
- [ ] Rotate MinIO credentials and restrict bucket policy
