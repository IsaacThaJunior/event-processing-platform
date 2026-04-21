# Task Queue System (Go + Redis + PostgreSQL)

A resilient background task processing system built in Go. Tasks are submitted via HTTP, queued in Redis by priority, and processed by a worker pool with retries, scheduling, chaining, and cancellation support.

---

## Architecture

```
HTTP Client
    │
    ▼
┌─────────────────────────────────────────┐
│              HTTP API (:8080)           │
│  POST /tasks       (create task)        │
│  DELETE /tasks/:id (cancel task)        │
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
│  │  on success → enqueue next task │    │
│  │  on failure → retry w/ backoff  │    │
│  │  max retries → DLQ              │    │
│  └─────────────────────────────────┘    │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│              PostgreSQL                 │
│  events            (task state)         │
│  event_delivery_logs (retry history)    │
│  idempotency_keys  (dedup)              │
└─────────────────────────────────────────┘
```

---

## Features

| Feature | Details |
|---|---|
| Priority queues | `high`, `medium`, `low` — workers drain high before low |
| Scheduled tasks | `execute_at` field defers a task to a future time |
| Task chaining | `next` field runs tasks sequentially after each succeeds |
| Task cancellation | `DELETE /tasks/:id` cancels any pending task |
| Idempotency | Duplicate requests with the same type/payload/priority are rejected |
| Retry + backoff | Up to 5 attempts: 1s → 2s → 4s → 8s → 16s |
| Dead letter queue | Tasks exceeding max retries are moved to DLQ |
| Delivery logs | Every attempt (success, retry, failure) is logged to postgres |
| Trace IDs | Each request carries a trace ID propagated through logs |
| Prometheus metrics | Exposed at `/metrics` |

---

## Task Types

| Type | Required payload fields |
|---|---|
| `resize_image` | `image_url`, `width`, `height` |
| `scrape_url` | `url` |
| `generate_report` | `date` |

---

## API

### Create a task

```
POST /tasks
```

```json
{
  "type": "scrape_url",
  "priority": "high",
  "payload": { "url": "https://example.com" }
}
```

Optional fields:

```json
{
  "execute_at": "2026-05-01T10:00:00Z",
  "next": {
    "type": "generate_report",
    "priority": "low",
    "payload": { "date": "2026-05-01" }
  }
}
```

**Chaining** — the `next` field is recursive. Tasks in the chain share the same `parent_id` (the root task's ID) and run sequentially after each step succeeds. A failure stops the chain at that step.

**Responses:**

| Status | Meaning |
|---|---|
| `200` | Task accepted and queued |
| `400` | Invalid request body or payload |
| `409` | Duplicate request (idempotency collision) |
| `500` | Internal error |

---

### Cancel a task

```
DELETE /tasks/:id
```

Only tasks in `pending` state can be cancelled. Tasks that are already processing, processed, or failed return `409`.

**Response:**

```json
{ "status": "cancelled", "event_id": "..." }
```

---

## Project Structure

```
.
├── cmd/
│   └── main.go                  # wires everything together, registers routes
├── internal/
│   ├── handler/
│   │   ├── task_handler.go      # HandleCreateTask, HandleCancelTask
│   │   └── task_sanitizer.go    # payload sanitization
│   ├── worker/
│   │   └── pool.go              # worker pool, retry logic, task routing
│   ├── repository/
│   │   ├── event_repository.go  # postgres event operations
│   │   └── redis_queue.go       # redis queue operations
│   ├── service/
│   │   ├── idempotency.go       # idempotency key management
│   │   └── task_validator.go    # per-type payload validation
│   ├── domain/
│   │   └── queue.go             # Queue interface
│   ├── middleware/
│   │   └── trace.go             # injects trace ID into request context
│   ├── sender/
│   │   └── response.go          # JSON response helpers
│   ├── metrics/
│   │   └── metrics.go           # Prometheus counters and histograms
│   └── database/                # sqlc-generated code (do not edit)
├── sql/
│   ├── schema/                  # goose migration files
│   └── queries/                 # sqlc query definitions
├── docker-compose.dev.yml
├── Dockerfile.dev
└── go.mod
```

---

## How to Run

**1. Start services**

```bash
docker compose up --build
```

Starts PostgreSQL, Redis, and the application on `:8080`.

**2. Run migrations**

```bash
goose -dir ./sql/schema postgres "postgres://user:pass@localhost:5432/db?sslmode=disable" up
```

**3. Regenerate SQLC code** (only after editing `sql/queries/`)

```bash
sqlc generate
```

**4. Submit a task**

```bash
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "type": "resize_image",
    "priority": "high",
    "payload": { "image_url": "https://picsum.photos/300", "width": 600, "height": 400 }
  }'
```

**5. Check metrics**

```
http://localhost:8080/metrics
```

---

## Retry Strategy

```
attempt:  1    2    3    4     5
delay:    1s → 2s → 4s → 8s → 16s
```

After 5 failed attempts the event is moved to the dead letter queue (`events_queue:dlq`) and its status is set to `failed` in postgres.

---

## Design Decisions

| Decision | Why | Trade-off |
|---|---|---|
| Redis lists + sorted set | Simple priority queue with scheduled task support | No built-in durability; at-least-once delivery |
| Idempotency keys in postgres | Dedup across restarts | Extra DB read on every request |
| Worker pool (fixed size) | Controlled concurrency, predictable DB load | Requires tuning for throughput |
| Task chaining via `next` | Sequential pipelines in a single request | Chain stops on first failure |
| `parent_id` = root task ID | All chain members traceable to origin in O(1) | Slightly denormalized |
| Exponential backoff | Protect downstream on transient failures | Slower recovery at high retry counts |
| SQLC | Compile-time SQL validation, no ORM overhead | Must regenerate after query changes |

---

## Production Considerations

- Use Redis Streams or Kafka for stronger delivery guarantees (ordered, replayable)
- Add distributed tracing (OpenTelemetry) for cross-service visibility
- Externalize configuration (env vars or config file)
- Add authentication on the HTTP layer
- Monitor the DLQ — a growing DLQ means a systemic processing failure
- Scale workers horizontally; the pool size should be tuned to DB connection pool limits
