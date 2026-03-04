# Event Processing System (Go + Redis + PostgreSQL)
A resilient background event processing system built in Go.
This project demonstrates production-grade backend patterns including:

- Worker pools
- Redis-backed queuing
- Exponential retry with backoff
- Dead-letter queue (DLQ)
- Structured logging
- Metrics endpoint
- Graceful shutdown
- Type-safe SQL with SQLC
----------
## Architecture

Here is the high level architecture for this project:


    Producer → Redis Queue → Worker Pool → Business Logic → PostgreSQL
                                           ↘ Dead Letter Queue
----------
## Core Components
1. **Redis Queue**

Redis is used as a lightweight message broker.

- `LPUSH` → enqueue event
- `RPOP` → dequeuing events
- Separate list for dead-letter queue

**Why Redis?**

- Simple
- Fast
- Minimal operational overhead
- Ideal for background job systems


----------
2. **Worker Pool**

A fixed-size worker pool processes events concurrently.
Each worker:

- Pulls events from Redis
- Processes event
- Retries on failure
- Logs delivery status
- Moves event to DLQ after max retries

**Why a worker pool?**

- Controlled concurrency
- Predictable resource usage
- Prevents DB overload
----------

**3. Retry with Exponential Backoff**
Retry strategy:

    delay = baseDelay * 2^(attempt-1)

Example:
1s → 2s → 4s → 8s → 16s
**Why?**

- Protects downstream systems
- Industry-standard retry strategy (or so I think)
----------

**4. Dead Letter Queue (DLQ)**
After maximum retry attempts, failed events are pushed to a separate Redis list.
**Why?**

- Prevent infinite retry loops
- Enable later inspection or replay
----------

**5. Idempotency**
Each event has a unique ID.
Before processing:

- We check if the event was already processed
- If yes → skip

**Why?**
Redis guarantees at-least-once delivery.
Idempotency prevents duplicate side effects.

----------

**6. PostgreSQL + SQLC**
Postgres stores:

- Processed events
- Delivery attempts
- Error messages
- Processing status

SQLC generates type-safe Go code from SQL queries.
**Why SQLC?**

- Compile-time query validation
- No runtime string-based queries
- Clean repository layer
----------

**7. Structured Logging**
Uses Go’s `log/slog` for structured logs:

    {
      "level": "INFO",
      "event_id": "task-99",
      "worker": 2,
      "status": "processed"
    }

**Why structured logging?**

- Machine-readable
- Searchable in log aggregation systems
- Production-friendly
----------

**8. Metrics Endpoint**
Exposes runtime metrics:

- total_processed
- total_failed
- total_retried
- queue_depth

Accessible at:

    http://localhost:8080/metrics
----------

**9. Graceful Shutdown**
On SIGINT/SIGTERM:

- Stop accepting new work
- Let in-flight jobs complete
- Close DB connections
- Exit cleanly

This ensures safe container termination.

----------
# Project Structure
    .
    ├── cmd/
    │   └── main.go
    ├── internal/
    │   ├── worker/
    │   ├── repository/
    │   ├── queue/
    │   ├── metrics/
    │   └── db/
    ├── sql/
    │   ├── schema/
    │   └── queries/
    ├── docker-compose.dev.yml
    ├── Dockerfile.dev
    └── go.mod
----------
## How to Run

**1. Start Services**

    docker compose -f docker-compose.dev.yml up --build

This starts:

- PostgreSQL
- Redis
- Application
----------

**2. Run Database Migrations**


    goose -dir ./sql/schema postgres "postgres://user:pass@localhost:5432/db?sslmode=disable" up
----------

**3. Generate SQLC Code**


    sqlc generate
----------

**4. Push an Event**
If using Docker:

    docker exec -it <redis-container> redis-cli

Then:

    LPUSH events_queue "task-99"

Workers will automatically process the event.

----------
## 5. Check Metrics

Open:

    http://localhost:8080/metrics
----------
## Design Decisions & Trade-offs
| Decision            | Why                        | Trade-off                                  |
| ------------------- | -------------------------- | ------------------------------------------ |
| Redis Lists         | Simple queue               | No built-in durability guarantees          |
| Worker Pool         | Controlled concurrency     | Requires tuning (Sometime later)           |
| Exponential Backoff | Avoid overload             | Higher retry latency                       |
| DLQ                 | Safety for poison messages | Requires monitoring                        |
| Idempotency         | Prevent duplicates         | Extra DB check                             |
| Structured Logging  | Production-ready logs      | Slight verbosity                           |
| In-memory Metrics   | Simple                     | Reset on restart (Will do something later) |

----------
## Production Considerations

For production deployment:

- Add Prometheus metrics
- Add distributed tracing (OpenTelemetry)
- Use Redis Streams or Kafka for stronger delivery guarantees
- Externalize configuration
- Add authentication/authorization
- Implement horizontal scaling
----------

# What This Project Demonstrates
- Concurrency control in Go
- Reliable background processing
- Failure handling patterns
- Idempotent system design
- Clean layered architecture
- Operational observability
- Production-oriented backend thinking
----------


