# Go Job Runner - Detailed Implementation Guide

This document breaks down each implementation step with exact file paths, structs, function signatures, Go concepts to learn, and acceptance criteria.

---

## Step 1: Project Setup

### Goal
Get a running Go project with PostgreSQL accessible locally, a working `main.go` that starts an HTTP server, and all dependencies installed.

### Tasks

#### 1.1 Docker Compose for PostgreSQL
Create `docker-compose.yml` at the project root.

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: jobrunner
      POSTGRES_PASSWORD: jobrunner
      POSTGRES_DB: jobrunner
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data

volumes:
  pgdata:
```

Run `docker compose up -d` to start PostgreSQL.

#### 1.2 Install Dependencies
```bash
go get github.com/go-chi/chi/v5
go get github.com/jackc/pgx/v5
go get github.com/google/uuid
go get github.com/joho/godotenv
```

#### 1.3 Environment Configuration
Create `.env` at the project root:
```
DATABASE_URL=postgres://jobrunner:jobrunner@localhost:5432/jobrunner?sslmode=disable
SERVER_PORT=8080
WORKER_COUNT=5
QUEUE_SIZE=100
```

Implement `internal/config/config.go`:
- Define a `Config` struct with fields: `DatabaseURL`, `ServerPort`, `WorkerCount`, `QueueSize`
- Write a `Load()` function that reads from environment variables (using `os.Getenv`)
- Use `godotenv.Load()` to load `.env` in development
- Provide sensible defaults for each field

#### 1.4 Logger Setup
Implement `internal/logger/logger.go`:
- Create a `NewLogger()` function that returns a `*slog.Logger`
- Use `slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})`
- This gives structured JSON logging to stdout

#### 1.5 Basic Main Entry Point
Implement `cmd/server/main.go`:
- Call `config.Load()` to get configuration
- Call `logger.NewLogger()` to get a logger
- Create a Chi router with `chi.NewRouter()`
- Add a health check route: `GET /health` returns `{"status": "ok"}`
- Start the HTTP server on the configured port
- Log a startup message with the port number

### Go Concepts
- **Go modules**: `go.mod` declares the module path and dependencies. `go get` adds dependencies.
- **Environment variables**: `os.Getenv()` reads them. The `godotenv` package loads `.env` files.
- **`log/slog`**: Go's structured logging package (added in Go 1.21). Produces key-value log entries. JSON handler makes logs machine-parsable.
- **Chi router**: A lightweight, idiomatic HTTP router for Go. Compatible with `net/http`.

### Acceptance Criteria
- [ ] `docker compose up -d` starts PostgreSQL successfully
- [ ] `go build ./cmd/server` compiles without errors
- [ ] Running the server and hitting `GET /health` returns `{"status": "ok"}`
- [ ] Startup log message appears in JSON format

---

## Step 2: Define Models

### Goal
Define the core data structures that represent jobs and job attempts throughout the system. These structs will be used by every layer (API, service, repository).

### Tasks

#### 2.1 Job Status Type
Update `internal/model/job.go`:

Define `JobStatus` as a custom string type with these constants:
- `StatusPending` = `"pending"` - Job created, waiting to be picked up
- `StatusQueued` = `"queued"` - Job placed in the worker queue
- `StatusRunning` = `"running"` - Worker is currently executing the job
- `StatusSuccess` = `"success"` - Job completed successfully
- `StatusFailed` = `"failed"` - Job failed (may be retried)
- `StatusCancelled` = `"cancelled"` - Job was cancelled by user

Add a `Valid()` method on `JobStatus` that returns `bool` - checks if the status is one of the known values.

Add an `IsTerminal()` method that returns `true` for `success`, `failed`, `cancelled` - these are end states.

#### 2.2 Job Struct
In the same file, define the `Job` struct:

| Field          | Type              | JSON tag        | DB column       | Description                          |
| -------------- | ----------------- | --------------- | --------------- | ------------------------------------ |
| ID             | `uuid.UUID`       | `"id"`          | `id`            | Primary key                          |
| Type           | `string`          | `"type"`        | `type`          | Job type (email, report, webhook)    |
| Payload        | `json.RawMessage` | `"payload"`     | `payload`       | Arbitrary JSON data for the executor |
| Status         | `JobStatus`       | `"status"`      | `status`        | Current job status                   |
| ScheduledAt    | `*time.Time`      | `"scheduled_at"`| `scheduled_at`  | When to run (nil = immediately)      |
| AttemptCount   | `int`             | `"attempt_count"`| `attempt_count`| How many times this job has been tried |
| MaxRetries     | `int`             | `"max_retries"` | `max_retries`   | Maximum retry attempts allowed       |
| LastError      | `*string`         | `"last_error"`  | `last_error`    | Error from the most recent attempt   |
| CreatedAt      | `time.Time`       | `"created_at"`  | `created_at`    | When the job was created             |
| UpdatedAt      | `time.Time`       | `"updated_at"`  | `updated_at`    | When the job was last modified       |

Use pointer types (`*time.Time`, `*string`) for nullable fields.

#### 2.3 Job Attempt Struct
Define `JobAttempt` in `internal/model/job_attempt.go`:

| Field          | Type          | JSON tag          | DB column       | Description                         |
| -------------- | ------------- | ----------------- | --------------- | ----------------------------------- |
| ID             | `uuid.UUID`   | `"id"`            | `id`            | Primary key                         |
| JobID          | `uuid.UUID`   | `"job_id"`        | `job_id`        | Foreign key to jobs table           |
| AttemptNumber  | `int`         | `"attempt_number"`| `attempt_number`| Which attempt this is (1, 2, 3...)  |
| Status         | `JobStatus`   | `"status"`        | `status`        | Result of this attempt              |
| ErrorMessage   | `*string`     | `"error_message"` | `error_message` | Error if this attempt failed        |
| StartedAt      | `time.Time`   | `"started_at"`    | `started_at`    | When execution started              |
| FinishedAt     | `*time.Time`  | `"finished_at"`   | `finished_at`   | When execution ended (nil if running) |

#### 2.4 Request/Response Types
Define `CreateJobRequest` in `internal/model/request.go`:

| Field        | Type              | JSON tag        | Description                        |
| ------------ | ----------------- | --------------- | ---------------------------------- |
| Type         | `string`          | `"type"`        | Required: job type                 |
| Payload      | `json.RawMessage` | `"payload"`     | Required: job data                 |
| ScheduledAt  | `*time.Time`      | `"scheduled_at"`| Optional: when to run              |
| MaxRetries   | `int`             | `"max_retries"` | Optional: retry limit (default 3)  |

Add a `Validate()` method on `CreateJobRequest` that returns an `error`:
- `Type` must not be empty
- `Type` must be one of the known types (email, report, webhook)
- `Payload` must be valid JSON (use `json.Valid()`)
- `MaxRetries` must be between 0 and 10
- `ScheduledAt` if provided must be in the future

### Go Concepts
- **Custom types**: `type JobStatus string` creates a distinct type from `string`. This lets you attach methods and provides type safety (you can't accidentally pass a random string where a `JobStatus` is expected).
- **Pointer types for nullable fields**: `*time.Time` can be `nil` (representing SQL `NULL`), while `time.Time` always has a value (zero value is `0001-01-01`).
- **`json.RawMessage`**: Stores raw JSON without parsing it into a Go struct. Useful when the JSON shape varies by job type.
- **`json` struct tags**: Control how fields are serialized/deserialized. `json:"field_name"` maps the Go field to a JSON key. `json:"field_name,omitempty"` omits the field if it's the zero value.
- **Methods on types**: `func (s JobStatus) Valid() bool` - the `(s JobStatus)` part is the receiver. This attaches the method to the type so you can call `status.Valid()`.

### Acceptance Criteria
- [ ] All structs compile without errors
- [ ] `JobStatus("pending").Valid()` returns `true`
- [ ] `JobStatus("invalid").Valid()` returns `false`
- [ ] `StatusSuccess.IsTerminal()` returns `true`, `StatusRunning.IsTerminal()` returns `false`
- [ ] `CreateJobRequest` validation catches missing type, invalid JSON, retries > 10

---

## Step 3: Database Setup

### Goal
Create the database tables for jobs and job_attempts, and establish a connection pool from the application.

### Tasks

#### 3.1 SQL Migrations
Create `migrations/001_create_jobs.sql`:

```sql
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE jobs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    type VARCHAR(50) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    scheduled_at TIMESTAMPTZ,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 3,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_jobs_status ON jobs(status);
CREATE INDEX idx_jobs_scheduled_at ON jobs(scheduled_at) WHERE scheduled_at IS NOT NULL;
CREATE INDEX idx_jobs_type ON jobs(type);
```

Create `migrations/002_create_job_attempts.sql`:

```sql
CREATE TABLE job_attempts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    attempt_number INTEGER NOT NULL,
    status VARCHAR(20) NOT NULL,
    error_message TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    UNIQUE(job_id, attempt_number)
);

CREATE INDEX idx_job_attempts_job_id ON job_attempts(job_id);
```

Run these manually against your database:
```bash
psql postgres://jobrunner:jobrunner@localhost:5432/jobrunner -f migrations/001_create_jobs.sql
psql postgres://jobrunner:jobrunner@localhost:5432/jobrunner -f migrations/002_create_job_attempts.sql
```

#### 3.2 Database Connection
Create `internal/database/postgres.go`:

Write a `Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error)` function:
- Use `pgxpool.New(ctx, databaseURL)` to create a connection pool
- Call `pool.Ping(ctx)` to verify the connection works
- Return the pool (or error)

The pool handles connection management automatically - it opens/closes connections as needed, reuses idle connections, and is safe for concurrent use from multiple goroutines.

#### 3.3 Wire Database into Main
Update `cmd/server/main.go`:
- After loading config, call `database.Connect()` with the database URL
- Defer `pool.Close()` so the pool is cleaned up on shutdown
- Log success or fatal on failure

### Go Concepts
- **`pgxpool.Pool`**: A connection pool for PostgreSQL. Unlike a single connection, a pool manages multiple connections and hands them out to concurrent goroutines. This is essential because your HTTP server handles multiple requests simultaneously.
- **`context.Context`**: A mechanism for carrying deadlines, cancellation signals, and request-scoped values. Database calls accept a context so they can be cancelled if, for example, the HTTP request is cancelled. `context.Background()` is the root context used at application startup.
- **`defer`**: Schedules a function call to run when the surrounding function returns. `defer pool.Close()` ensures the pool is closed even if `main()` exits due to an error. Deferred calls execute in LIFO (last-in, first-out) order.
- **Indexes**: `CREATE INDEX` speeds up queries that filter on that column. Partial indexes (`WHERE scheduled_at IS NOT NULL`) only index rows matching the condition, saving space.
- **JSONB**: PostgreSQL's binary JSON type. Supports indexing and querying into the JSON structure. More efficient than `JSON` type for reads.

### Acceptance Criteria
- [ ] Both migrations run without errors
- [ ] Tables appear in the database (`\dt` in psql)
- [ ] Application starts and logs "connected to database" (or similar)
- [ ] Application exits cleanly with a helpful error if the database is unreachable

---

## Step 4: Repository Layer

### Goal
Build the data access layer that translates between Go structs and database rows. The repository interface allows swapping implementations (e.g., PostgreSQL for tests vs. in-memory for unit tests).

### Tasks

#### 4.1 Define the Repository Interface
Update `internal/repository/job_repository.go`:

```go
type JobRepository interface {
    CreateJob(ctx context.Context, job *model.Job) error
    GetJobByID(ctx context.Context, id uuid.UUID) (*model.Job, error)
    ListJobs(ctx context.Context, filter JobFilter) ([]model.Job, error)
    UpdateJobStatus(ctx context.Context, id uuid.UUID, status model.JobStatus) error
    UpdateJob(ctx context.Context, job *model.Job) error
    GetPendingJobs(ctx context.Context, limit int) ([]model.Job, error)
    GetScheduledJobs(ctx context.Context, before time.Time, limit int) ([]model.Job, error)

    InsertAttempt(ctx context.Context, attempt *model.JobAttempt) error
    GetAttemptsByJobID(ctx context.Context, jobID uuid.UUID) ([]model.JobAttempt, error)
}
```

Define `JobFilter` struct in the same file:

| Field   | Type            | Description                           |
| ------- | --------------- | ------------------------------------- |
| Status  | `*model.JobStatus` | Filter by status (nil = all)       |
| Type    | `*string`       | Filter by job type (nil = all)        |
| Limit   | `int`           | Max results (default 50)              |
| Offset  | `int`           | Pagination offset                     |

#### 4.2 Implement PostgreSQL Repository
Implement `internal/repository/postgres/job_repository.go`:

Define a `PostgresJobRepository` struct that holds a `*pgxpool.Pool`.

Write a constructor: `NewPostgresJobRepository(pool *pgxpool.Pool) *PostgresJobRepository`

Implement each method:

**`CreateJob`**: Insert a new row into the `jobs` table.
- Use `pool.QueryRow()` with `INSERT INTO jobs (...) VALUES (...) RETURNING id, created_at, updated_at`
- Scan the returned values back into the job struct
- This pattern lets the database generate the UUID and timestamps

**`GetJobByID`**: Fetch a single job by its UUID.
- Use `pool.QueryRow()` with `SELECT ... FROM jobs WHERE id = $1`
- Return a custom error (define `ErrJobNotFound`) if `pgx.ErrNoRows` is returned
- Scan all columns into the Job struct

**`ListJobs`**: Fetch multiple jobs with optional filters.
- Build the query dynamically based on which filters are set
- Use a slice of `args` and increment `$1`, `$2`, etc. for each filter
- Always apply `ORDER BY created_at DESC` and `LIMIT`/`OFFSET`

**`UpdateJobStatus`**: Update only the status and `updated_at` columns.
- Use `pool.Exec()` with `UPDATE jobs SET status = $1, updated_at = NOW() WHERE id = $2`
- Check `commandTag.RowsAffected()` - if 0, the job doesn't exist

**`UpdateJob`**: Update multiple fields on a job.
- Update `status`, `attempt_count`, `last_error`, `updated_at`
- Used after a job attempt completes to save all changes at once

**`GetPendingJobs`**: Fetch jobs ready to be queued.
- `SELECT ... FROM jobs WHERE status = 'pending' AND scheduled_at IS NULL ORDER BY created_at ASC LIMIT $1`
- These are jobs with no schedule that should run immediately

**`GetScheduledJobs`**: Fetch scheduled jobs whose time has come.
- `SELECT ... FROM jobs WHERE status = 'pending' AND scheduled_at IS NOT NULL AND scheduled_at <= $1 ORDER BY scheduled_at ASC LIMIT $2`
- The `before` parameter is typically `time.Now()`

**`InsertAttempt`**: Insert a row into `job_attempts`.
- Similar pattern to `CreateJob`

**`GetAttemptsByJobID`**: Fetch all attempts for a job, ordered by attempt number.
- `SELECT ... FROM job_attempts WHERE job_id = $1 ORDER BY attempt_number ASC`

#### 4.3 Define Custom Errors
Create `internal/repository/errors.go`:

```go
var (
    ErrJobNotFound = errors.New("job not found")
    ErrNoRowsAffected = errors.New("no rows affected")
)
```

### Go Concepts
- **Interfaces**: `type JobRepository interface { ... }` defines a contract without implementation. Any struct that implements all the methods satisfies the interface. This enables dependency injection: the service layer depends on the interface, not the concrete PostgreSQL implementation.
- **Dependency injection**: The repository receives its dependency (the `*pgxpool.Pool`) through the constructor. It doesn't create its own connection. This makes it testable (you can pass a test pool) and flexible (you can swap implementations).
- **`pgx` query patterns**: 
  - `QueryRow()` for single-row results, followed by `.Scan()` to read columns into Go variables
  - `Query()` for multi-row results, followed by a `for rows.Next()` loop
  - `Exec()` for INSERT/UPDATE/DELETE when you don't need returned data
  - `$1`, `$2` are PostgreSQL parameter placeholders (prevents SQL injection)
- **Sentinel errors**: `var ErrJobNotFound = errors.New(...)` creates a named error that callers can check with `errors.Is(err, ErrJobNotFound)`. This is cleaner than comparing error strings.
- **`errors.Is()`**: Use this instead of `==` to compare errors. It unwraps wrapped errors to check the chain. `if errors.Is(err, pgx.ErrNoRows)` handles cases where the pgx error might be wrapped.

### Acceptance Criteria
- [ ] Code compiles with `go build ./...`
- [ ] `PostgresJobRepository` satisfies the `JobRepository` interface (compiler enforces this)
- [ ] Can create a job and retrieve it by ID
- [ ] ListJobs returns filtered results correctly
- [ ] GetPendingJobs only returns pending jobs with no schedule
- [ ] GetScheduledJobs only returns jobs whose scheduled_at has passed
- [ ] GetJobByID returns `ErrJobNotFound` for a non-existent UUID

---

## Step 5: Service Layer

### Goal
Implement the business logic layer that sits between the HTTP handlers and the repository. The service enforces rules like valid status transitions, retry limits, and cancellation logic.

### Tasks

#### 5.1 Define the Service
Implement `internal/service/job_service.go`:

Define a `JobService` struct with dependencies:
- `repo repository.JobRepository` - for database access
- `logger *slog.Logger` - for structured logging

Write constructor: `NewJobService(repo repository.JobRepository, logger *slog.Logger) *JobService`

#### 5.2 Implement Service Methods

**`CreateJob(ctx, req *model.CreateJobRequest) (*model.Job, error)`**:
- Call `req.Validate()` - return error if invalid
- Build a `model.Job` from the request:
  - Generate UUID with `uuid.New()`
  - Set `Status` to `StatusPending`
  - Set `MaxRetries` to `req.MaxRetries` (default 3 if 0)
  - Set `CreatedAt` and `UpdatedAt` to `time.Now()`
- Call `repo.CreateJob(ctx, job)`
- Log the creation with job ID and type
- Return the created job

**`GetJob(ctx, id uuid.UUID) (*model.Job, error)`**:
- Call `repo.GetJobByID(ctx, id)`
- Return the job or propagate the error

**`ListJobs(ctx, filter repository.JobFilter) ([]model.Job, error)`**:
- Set default limit if not provided (e.g., 50)
- Cap maximum limit (e.g., 100) to prevent unbounded queries
- Call `repo.ListJobs(ctx, filter)`

**`CancelJob(ctx, id uuid.UUID) error`**:
- Fetch the job by ID
- Check if the job can be cancelled: only `pending`, `queued`, or `running` jobs
- If the status is terminal (`success`, `failed`, `cancelled`), return an error like `"cannot cancel job in terminal state"`
- Update the job status to `StatusCancelled`
- Log the cancellation

**`RetryJob(ctx, id uuid.UUID) (*model.Job, error)`**:
- Fetch the job by ID
- Only allow retry if status is `failed`
- Reset `Status` to `StatusPending`
- Reset `AttemptCount` to 0 (or keep it and just bump `MaxRetries`)
- Update the job in the database
- Log the retry
- Return the updated job

**`GetJobHistory(ctx, jobID uuid.UUID) ([]model.JobAttempt, error)`**:
- First verify the job exists (call `GetJob`)
- Call `repo.GetAttemptsByJobID(ctx, jobID)`

#### 5.3 Status Transition Validation
Add a helper function or method that validates status transitions:

```
Valid transitions:
  pending  -> queued, cancelled
  queued   -> running, cancelled
  running  -> success, failed, cancelled
  failed   -> pending (via retry)
```

Create a `ValidTransition(from, to JobStatus) bool` function. Use a map:
```go
var validTransitions = map[model.JobStatus][]model.JobStatus{
    model.StatusPending:  {model.StatusQueued, model.StatusCancelled},
    model.StatusQueued:   {model.StatusRunning, model.StatusCancelled},
    model.StatusRunning:  {model.StatusSuccess, model.StatusFailed, model.StatusCancelled},
    model.StatusFailed:   {model.StatusPending},
}
```

Use this in `UpdateJobStatus` to reject invalid transitions.

### Go Concepts
- **Service layer pattern**: The service sits between HTTP handlers and the database. Handlers deal with HTTP concerns (parsing requests, writing responses). Services deal with business rules (can this job be cancelled?). Repositories deal with data access. This separation makes each layer independently testable.
- **Error wrapping**: Use `fmt.Errorf("failed to create job: %w", err)` to wrap errors with context. The `%w` verb wraps the original error so callers can still use `errors.Is()` to check the underlying cause. This gives you both a readable message and programmatic error checking.
- **Method receivers - pointer vs value**: `func (s *JobService) CreateJob(...)` uses a pointer receiver (`*JobService`). Use pointer receivers when the method needs to modify the receiver or when the struct is large. Use value receivers for small, immutable structs. Since `JobService` holds a logger and repo (and might be extended), pointer receivers are correct here.
- **Structured logging with slog**: `s.logger.Info("job created", "job_id", job.ID, "type", job.Type)` logs a message with key-value pairs. This produces searchable, parseable log entries. Always include relevant IDs in log messages.
- **Map-based validation**: Using a map to define valid transitions is cleaner than a switch statement and easier to extend. It also makes the rules visible in one place.

### Acceptance Criteria
- [ ] Creating a job with invalid input returns a descriptive error
- [ ] Creating a valid job returns a job with `pending` status and a generated UUID
- [ ] Cancelling a `pending` job succeeds
- [ ] Cancelling a `success` job returns an error
- [ ] Retrying a `failed` job resets it to `pending`
- [ ] Retrying a `running` job returns an error
- [ ] `ValidTransition("pending", "running")` returns `false` (must go through `queued`)

---

## Step 6: HTTP API Layer

### Goal
Build the REST API handlers and middleware. Handlers parse HTTP requests, call the service layer, and write HTTP responses. Middleware adds cross-cutting concerns like logging and request IDs.

### Tasks

#### 6.1 Middleware
Implement `internal/api/middleware.go`:

**Request ID Middleware**:
- Generate a UUID for each request (or read from `X-Request-ID` header if present)
- Store it in the request context using `context.WithValue()`
- Set it on the response as `X-Request-ID` header
- Define a context key type: `type ctxKey string` and `const RequestIDKey ctxKey = "request_id"`

**Logging Middleware**:
- Log the start of each request: method, path, request ID
- Use `http.ResponseWriter` wrapper to capture the status code
- Log the completion: method, path, status code, duration
- Use `time.Since(start)` for duration

**Recovery Middleware** (panic recovery):
- Use `defer func() { if r := recover(); r != nil { ... } }()`
- Log the panic with stack trace
- Return 500 Internal Server Error to the client
- This prevents a panic in one handler from crashing the entire server

#### 6.2 Response Helpers
Create `internal/api/response.go`:

```go
func writeJSON(w http.ResponseWriter, status int, data any) { ... }
func writeError(w http.ResponseWriter, status int, message string) { ... }
```

- `writeJSON`: Set `Content-Type: application/json`, write status code, encode data as JSON
- `writeError`: Write a JSON error response `{"error": "message"}`

#### 6.3 Handler Struct
Update `internal/api/handler.go`:

Define a `Handler` struct with dependencies:
- `service *service.JobService`
- `logger *slog.Logger`

Constructor: `NewHandler(service *service.JobService, logger *slog.Logger) *Handler`

#### 6.4 Implement Handlers

**`POST /jobs` - CreateJob**:
- Decode request body into `model.CreateJobRequest` using `json.NewDecoder(r.Body).Decode()`
- Return 400 if body is invalid or fails validation
- Call `service.CreateJob(ctx, &req)`
- Return 201 Created with the job as JSON

**`GET /jobs/:id` - GetJob**:
- Extract `id` from URL using `chi.URLParam(r, "id")`
- Parse as UUID - return 400 if invalid
- Call `service.GetJob(ctx, id)`
- If `ErrJobNotFound`, return 404. Otherwise return 200 with the job.

**`GET /jobs` - ListJobs**:
- Parse query parameters: `status`, `type`, `limit`, `offset`
- Build a `repository.JobFilter` from the parameters
- Call `service.ListJobs(ctx, filter)`
- Return 200 with the job list (even if empty - return `[]`, not `null`)

**`POST /jobs/:id/cancel` - CancelJob**:
- Extract and parse the job ID
- Call `service.CancelJob(ctx, id)`
- If error mentions "terminal state", return 409 Conflict
- If `ErrJobNotFound`, return 404
- Otherwise return 200 with `{"message": "job cancelled"}`

**`POST /jobs/:id/retry` - RetryJob**:
- Extract and parse the job ID
- Call `service.RetryJob(ctx, id)`
- Return 200 with the updated job

**`GET /jobs/:id/history` - GetJobHistory**:
- Extract and parse the job ID
- Call `service.GetJobHistory(ctx, id)`
- Return 200 with the attempts list

#### 6.5 Route Registration
Update `internal/api/routes.go`:

```go
func SetupRoutes(handler *Handler, logger *slog.Logger) *chi.Mux {
    r := chi.NewRouter()

    // Middleware
    r.Use(RequestIDMiddleware)
    r.Use(LoggingMiddleware(logger))
    r.Use(RecoveryMiddleware(logger))

    // Routes
    r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
    })

    r.Route("/jobs", func(r chi.Router) {
        r.Post("/", handler.CreateJob)
        r.Get("/", handler.ListJobs)
        r.Get("/{id}", handler.GetJob)
        r.Post("/{id}/cancel", handler.CancelJob)
        r.Post("/{id}/retry", handler.RetryJob)
        r.Get("/{id}/history", handler.GetJobHistory)
    })

    return r
}
```

#### 6.6 Wire Everything in Main
Update `cmd/server/main.go` to create all dependencies and start the server:
- Create the repository (passing the DB pool)
- Create the service (passing the repository and logger)
- Create the handler (passing the service and logger)
- Set up routes
- Start the HTTP server

### Go Concepts
- **`http.Handler` and `http.HandlerFunc`**: Go's HTTP interfaces. A `Handler` has a `ServeHTTP(w, r)` method. `HandlerFunc` is an adapter that lets you use regular functions as handlers. Chi uses these under the hood.
- **Middleware pattern**: Middleware wraps a handler to add behavior. It's a function that takes an `http.Handler` and returns a new `http.Handler`. Middleware runs before and/or after the actual handler. `r.Use()` applies middleware to all routes.
- **`context.WithValue()`**: Stores a key-value pair in the request context. Used to pass request-scoped data (like request ID) through middleware to handlers. Always use a custom type for the key to avoid collisions: `type ctxKey string`.
- **`chi.URLParam()`**: Extracts path parameters from the URL. `/{id}` in the route definition matches anything, and `chi.URLParam(r, "id")` retrieves the matched value.
- **`json.NewDecoder` vs `json.Unmarshal`**: `NewDecoder` reads from a stream (`r.Body`) and is more memory-efficient for HTTP request bodies. `Unmarshal` works on byte slices that are already in memory.
- **Response status codes**: 
  - 200 OK: successful retrieval/action
  - 201 Created: successful creation
  - 400 Bad Request: malformed input
  - 404 Not Found: resource doesn't exist
  - 409 Conflict: action conflicts with current state
  - 500 Internal Server Error: unexpected failure
- **`r.Route()` group**: Chi's route grouping. `r.Route("/jobs", ...)` prefixes all nested routes with `/jobs`. Keeps route definitions organized.

### Acceptance Criteria
- [ ] `POST /jobs` with valid body returns 201 and a job with UUID
- [ ] `POST /jobs` with empty body returns 400
- [ ] `GET /jobs/{id}` with valid UUID returns 200
- [ ] `GET /jobs/{id}` with non-existent UUID returns 404
- [ ] `GET /jobs` returns a JSON array (even if empty)
- [ ] `POST /jobs/{id}/cancel` on a pending job returns 200
- [ ] Every response includes `X-Request-ID` header
- [ ] Logs show request method, path, status, and duration

---

## Step 7: Worker Pool

### Goal
Build a concurrent worker pool that consumes jobs from an in-memory queue and executes them. This is the core concurrency component of the system.

### Tasks

#### 7.1 Job Queue
Implement `internal/queue/queue.go`:

Define a `JobQueue` struct:
- `jobs chan uuid.UUID` - buffered channel holding job IDs to process
- `size int` - capacity of the queue

Constructor: `NewJobQueue(size int) *JobQueue`
- Create a buffered channel: `make(chan uuid.UUID, size)`

Methods:
- `Enqueue(jobID uuid.UUID) error` - Send a job ID into the channel. Use a `select` with `default` to return an error if the queue is full (non-blocking send).
- `Dequeue() <-chan uuid.UUID` - Return the read-only end of the channel. Workers will range over this.
- `Close()` - Close the channel. This signals all workers to stop.

#### 7.2 Worker Pool
Implement `internal/worker/pool.go`:

Define a `WorkerPool` struct:
- `queue *queue.JobQueue`
- `service *service.JobService` (to fetch jobs and update status)
- `executors map[string]Executor` (maps job type to its executor)
- `logger *slog.Logger`
- `wg sync.WaitGroup` (to wait for all workers to finish on shutdown)
- `cancelFuncs map[uuid.UUID]context.CancelFunc` (to cancel running jobs)
- `mu sync.Mutex` (protects the cancelFuncs map)

Constructor: `NewWorkerPool(queue, service, executors, logger) *WorkerPool`

**`Start(ctx context.Context, workerCount int)`**:
- Loop `workerCount` times
- For each worker:
  - Increment `wg` with `wg.Add(1)`
  - Launch a goroutine that calls `p.worker(ctx, workerID)`
- Log that the pool has started with the worker count

**`worker(ctx context.Context, id int)`**:
- Defer `wg.Done()`
- Log that the worker has started
- Range over `queue.Dequeue()`:
  - This blocks until a job ID is available or the channel is closed
  - When a job ID is received, call `p.processJob(ctx, jobID)`
  - When the channel is closed, the loop ends and the worker exits
- Log that the worker has stopped

**`processJob(ctx context.Context, jobID uuid.UUID)`**:
- Fetch the job from the database via `service.GetJob(ctx, jobID)`
- Look up the executor for `job.Type` in the executors map
  - If no executor found, mark the job as failed with error "unknown job type"
- Update job status to `running`
- Create a cancellable context: `jobCtx, cancel := context.WithCancel(ctx)`
- Store the cancel function: `p.mu.Lock(); p.cancelFuncs[jobID] = cancel; p.mu.Unlock()`
- Record the start of the attempt (insert a `JobAttempt` with status `running`)
- Call `executor.Execute(jobCtx, job)`
- Remove the cancel function from the map
- If error: update job to `failed`, update attempt to `failed`, record the error, check retry logic
- If success: update job to `success`, update attempt to `success`

**`CancelJob(jobID uuid.UUID) bool`**:
- Lock mutex, look up the cancel function, call it if found
- Return true if the job was running and cancel was triggered

**`Stop()`**:
- Close the queue (signals workers to exit)
- Call `wg.Wait()` to wait for all workers to finish current jobs
- Log that the pool has stopped

### Go Concepts
- **Goroutines**: `go func() { ... }()` launches a function on a new lightweight thread. Goroutines are multiplexed onto OS threads by the Go runtime. They're cheap to create (a few KB of stack) so you can have thousands.
- **Channels**: `chan uuid.UUID` is a typed pipe between goroutines. Sending (`ch <- value`) and receiving (`value := <-ch`) block until the other side is ready. This provides natural synchronization.
- **Buffered channels**: `make(chan uuid.UUID, 100)` creates a channel with a buffer of 100 items. Sends only block when the buffer is full. Acts as an in-memory queue with backpressure.
- **`range` over a channel**: `for jobID := range ch` receives values from the channel until it's closed. This is the idiomatic way to consume from a channel in a loop.
- **`select` with `default`**: Makes a channel operation non-blocking. Without `select`, sending to a full channel blocks forever. With `default`, it immediately falls through.
  ```go
  select {
  case p.jobs <- jobID:
      return nil
  default:
      return errors.New("queue is full")
  }
  ```
- **`sync.WaitGroup`**: Coordinates goroutine completion. `Add(1)` before launching, `Done()` when finished, `Wait()` blocks until the count reaches zero. Essential for graceful shutdown.
- **`sync.Mutex`**: Mutual exclusion lock. `Lock()`/`Unlock()` protects shared data from concurrent access. The `cancelFuncs` map is written by `processJob` and read by `CancelJob`, so it needs protection.
- **`context.WithCancel`**: Creates a child context with a cancel function. Calling the cancel function signals all code using that context to stop. The executor checks `ctx.Done()` to respond to cancellation.

### Acceptance Criteria
- [ ] Workers start and log their IDs
- [ ] Enqueuing a job ID causes a worker to pick it up
- [ ] `Enqueue` on a full queue returns an error (non-blocking)
- [ ] `Stop()` waits for running jobs to finish before returning
- [ ] `CancelJob` triggers cancellation on a running job
- [ ] Multiple workers can process jobs concurrently (test with multiple jobs)

---

## Step 8: Executors

### Goal
Define the Executor interface and implement concrete executors for different job types. Executors contain the actual work logic for each job type.

### Tasks

#### 8.1 Executor Interface
Update `internal/worker/executor.go`:

```go
type Executor interface {
    Execute(ctx context.Context, job *model.Job) error
}
```

The interface is intentionally simple - one method. This makes it easy to add new job types by implementing a single function.

#### 8.2 Email Executor
Create `internal/worker/executors/email.go`:

```go
type EmailExecutor struct {
    logger *slog.Logger
}
```

`Execute(ctx, job)`:
- Parse `job.Payload` to extract email fields (to, subject, body). Define a struct:
  ```go
  type EmailPayload struct {
      To      string `json:"to"`
      Subject string `json:"subject"`
      Body    string `json:"body"`
  }
  ```
- Validate the payload (non-empty fields)
- Simulate sending the email with `time.Sleep(2 * time.Second)`
- Check for context cancellation between steps:
  ```go
  select {
  case <-ctx.Done():
      return ctx.Err()
  default:
  }
  ```
- Log success
- Return nil on success, error on failure

#### 8.3 Report Executor
Create `internal/worker/executors/report.go`:

```go
type ReportExecutor struct {
    logger *slog.Logger
}
```

`Execute(ctx, job)`:
- Parse payload to extract report parameters (report_type, date_range, format)
- Simulate report generation in stages (each stage checks for cancellation):
  1. "Fetching data..." - `time.Sleep(1s)`
  2. "Processing data..." - `time.Sleep(2s)`
  3. "Generating output..." - `time.Sleep(1s)`
- Log each stage for observability
- Return nil on success

#### 8.4 Webhook Executor
Create `internal/worker/executors/webhook.go`:

```go
type WebhookExecutor struct {
    client *http.Client
    logger *slog.Logger
}
```

`Execute(ctx, job)`:
- Parse payload to extract webhook details (url, method, headers, body)
- Create an HTTP request using `http.NewRequestWithContext(ctx, ...)` - this ties the request to the job's context, so cancellation stops the HTTP call
- Set headers from payload
- Execute the request using `p.client.Do(req)`
- Check response status code - return error if not 2xx
- Log the response status

#### 8.5 Register Executors
In the worker pool setup (in `main.go` or a factory function), register all executors:

```go
executors := map[string]worker.Executor{
    "email":   executors.NewEmailExecutor(logger),
    "report":  executors.NewReportExecutor(logger),
    "webhook": executors.NewWebhookExecutor(&http.Client{Timeout: 30 * time.Second}, logger),
}
```

### Go Concepts
- **Interfaces in practice**: The `Executor` interface is a single-method interface (common in Go). Any struct with an `Execute(context.Context, *model.Job) error` method satisfies it. Adding a new job type means creating a new struct - no changes to existing code (Open/Closed Principle).
- **`json.Unmarshal` for payload parsing**: Each executor parses `job.Payload` differently. `json.Unmarshal(job.Payload, &emailPayload)` converts the raw JSON into a typed struct. Errors here mean the job was created with bad data.
- **Context-aware operations**: `http.NewRequestWithContext(ctx, ...)` creates an HTTP request that will be automatically cancelled if the context is cancelled. `time.Sleep` doesn't check context, so use the `select` pattern to check `ctx.Done()` between steps.
- **`ctx.Err()`**: Returns `context.Canceled` if the context was cancelled, or `context.DeadlineExceeded` if the deadline passed. Check this to distinguish between "job failed" and "job was cancelled".
- **`http.Client` with timeout**: `&http.Client{Timeout: 30 * time.Second}` sets a global timeout for all requests. This prevents webhook calls from hanging forever. The context cancellation is an additional, per-job cancellation mechanism.
- **Interface satisfaction check**: Add `var _ Executor = (*EmailExecutor)(nil)` at the top of each executor file. This compile-time check ensures your struct satisfies the interface. If you forget a method, you get a compile error instead of a runtime surprise.

### Acceptance Criteria
- [ ] Each executor compiles and satisfies the `Executor` interface
- [ ] Email executor parses payload and simulates sending
- [ ] Report executor logs each stage
- [ ] Webhook executor makes real HTTP calls (test with a request bin service)
- [ ] All executors respond to context cancellation within ~1 second
- [ ] Unknown job type results in a clear error, not a crash

---

## Step 9: Retry Logic

### Goal
When a job fails, automatically retry it if it has remaining retry attempts. Track each attempt in the database.

### Tasks

#### 9.1 Update processJob in Worker Pool
After a job execution fails, add retry logic in `internal/worker/pool.go`:

In the `processJob` method, after `executor.Execute()` returns an error:

```
1. Increment job.AttemptCount
2. Record the error in job.LastError
3. Insert a failed JobAttempt record
4. Check: was this a cancellation? (errors.Is(err, context.Canceled))
   - If yes: mark as cancelled, do NOT retry
5. Check: has AttemptCount reached MaxRetries?
   - If yes: mark as failed (permanently), log "max retries reached"
   - If no: mark as pending (will be picked up again), log "will retry"
6. Update the job in the database
```

#### 9.2 Retry Delay (Optional Enhancement)
Add a delay before retrying to avoid hammering a failing service:

- Add a `RetryDelay` field to the `Job` model (or compute it from attempt count)
- Use exponential backoff: `delay = baseDelay * 2^(attemptCount - 1)`
  - Attempt 1: 1s, Attempt 2: 2s, Attempt 3: 4s, Attempt 4: 8s
- Instead of immediately re-enqueuing, set `ScheduledAt` to `time.Now().Add(delay)`
- The scheduler (Step 10) will pick it up when the delay has passed
- Cap the maximum delay (e.g., 5 minutes)

#### 9.3 Update Service Layer
Add a method to `JobService`:

**`RecordAttempt(ctx, jobID uuid.UUID, status JobStatus, errMsg *string) error`**:
- Fetch the job to get the current attempt count
- Create a `JobAttempt` with `AttemptNumber = job.AttemptCount + 1`
- Set `StartedAt` and `FinishedAt`
- Call `repo.InsertAttempt(ctx, attempt)`

This centralizes attempt recording so the worker pool doesn't need to construct attempt objects directly.

### Go Concepts
- **Error type checking**: `errors.Is(err, context.Canceled)` checks if the error (or any error in its chain) is `context.Canceled`. This distinguishes "the executor code failed" from "we deliberately cancelled this job". Different cause, different handling.
- **Exponential backoff**: A retry strategy where the wait time doubles after each failure. This prevents a storm of retries from overwhelming a recovering service. The formula `baseDelay * 2^attempt` gives increasing intervals. Adding jitter (a random offset) prevents multiple retrying jobs from hitting the service simultaneously - this is called "jitter and backoff."
- **`time.Duration` arithmetic**: `time.Second * time.Duration(math.Pow(2, float64(attempt)))` computes the backoff. `time.Duration` is an `int64` nanosecond count, so you can multiply and add durations.
- **State machine**: The job status transitions form a state machine. The retry logic adds the `failed -> pending` transition (with conditions). Drawing this state machine helps reason about edge cases.

### Acceptance Criteria
- [ ] A failing job with `max_retries=3` is retried 3 times before being permanently marked as failed
- [ ] Each attempt is recorded in the `job_attempts` table
- [ ] A cancelled job is NOT retried (even if retries remain)
- [ ] `GET /jobs/:id/history` shows all attempts with their statuses and errors
- [ ] Retry delay increases with each attempt (if exponential backoff is implemented)
- [ ] After the final retry, `last_error` contains the error from the last attempt

---

## Step 10: Scheduler

### Goal
Build a scheduler loop that periodically checks for scheduled jobs whose execution time has arrived and enqueues them into the worker queue.

### Tasks

#### 10.1 Scheduler Struct
Create `internal/scheduler/scheduler.go`:

Define a `Scheduler` struct:
- `service *service.JobService`
- `queue *queue.JobQueue`
- `repo repository.JobRepository`
- `logger *slog.Logger`
- `interval time.Duration` (how often to check, e.g., 5 seconds)

Constructor: `NewScheduler(service, queue, repo, logger, interval) *Scheduler`

#### 10.2 Scheduler Loop

**`Start(ctx context.Context)`**:
- Create a `time.NewTicker(s.interval)`
- Defer `ticker.Stop()`
- Loop:
  ```go
  for {
      select {
      case <-ctx.Done():
          s.logger.Info("scheduler stopped")
          return
      case <-ticker.C:
          s.poll(ctx)
      }
  }
  ```

**`poll(ctx context.Context)`**:
- Call `repo.GetScheduledJobs(ctx, time.Now(), batchSize)` to get jobs whose `scheduled_at` has passed
- Also call `repo.GetPendingJobs(ctx, batchSize)` to get non-scheduled pending jobs
- For each job:
  - Update status to `queued`
  - Try to enqueue the job ID into the queue
  - If enqueue fails (queue full), revert status back to `pending`
  - Log each job that gets enqueued

#### 10.3 Prevent Double-Processing
There's a race condition: between fetching pending jobs and enqueuing them, another scheduler instance (or the same one on the next tick) might pick up the same job.

Solutions:
- **Optimistic approach**: Update the job status to `queued` before enqueuing. If the status update succeeds, you "own" the job. Use `UPDATE jobs SET status = 'queued' WHERE id = $1 AND status = 'pending'` - the `AND status = 'pending'` clause acts as a compare-and-swap.
- Add a `ClaimPendingJobs` method to the repository that atomically selects and updates in one query:
  ```sql
  UPDATE jobs SET status = 'queued', updated_at = NOW()
  WHERE id IN (
      SELECT id FROM jobs
      WHERE status = 'pending' AND (scheduled_at IS NULL OR scheduled_at <= $1)
      ORDER BY created_at ASC
      LIMIT $2
      FOR UPDATE SKIP LOCKED
  )
  RETURNING id
  ```
  `FOR UPDATE SKIP LOCKED` is PostgreSQL-specific: it locks the selected rows but skips rows already locked by another transaction, preventing deadlocks.

### Go Concepts
- **`time.Ticker`**: Sends the current time on its channel at regular intervals. `ticker.C` is the channel. Always `defer ticker.Stop()` to release resources. Unlike `time.After`, a Ticker resets automatically.
- **`select` statement**: Waits on multiple channel operations simultaneously. The first channel that's ready wins. This is Go's multiplexing primitive. In the scheduler, it waits for either the ticker to fire or the context to be cancelled.
- **`context.Done()`**: Returns a channel that's closed when the context is cancelled. Use it in `select` to respond to shutdown signals. This is how the scheduler knows to stop gracefully.
- **`FOR UPDATE SKIP LOCKED`**: A PostgreSQL row-locking strategy. `FOR UPDATE` locks selected rows so other transactions can't modify them. `SKIP LOCKED` makes other transactions skip already-locked rows instead of waiting. This is ideal for job queues - multiple consumers can safely grab different jobs.
- **Race conditions**: When multiple goroutines (or processes) access shared state without synchronization, results are unpredictable. The scheduler's "fetch then update" is a classic race. Atomic database operations (single UPDATE query) eliminate the race at the database level.

### Acceptance Criteria
- [ ] Creating a job with `scheduled_at` in the future: job stays `pending` until the time arrives
- [ ] After `scheduled_at` passes, the scheduler picks up and enqueues the job
- [ ] Non-scheduled pending jobs are picked up immediately
- [ ] The scheduler stops cleanly when its context is cancelled
- [ ] No duplicate processing: a job is only enqueued once (verify with logs)

---

## Step 11: Job Cancellation

### Goal
Enable users to cancel running jobs. Pending/queued jobs can be cancelled by updating their status. Running jobs require context cancellation to interrupt the executor.

### Tasks

#### 11.1 Update the Cancel Flow
The worker pool already has `cancelFuncs map[uuid.UUID]context.CancelFunc` from Step 7. Now wire it to the API.

Update `internal/service/job_service.go` - the `CancelJob` method needs access to the worker pool's cancel mechanism.

Options for connecting the service to the worker pool:
- **Option A**: Pass a `Canceller` interface to the service:
  ```go
  type Canceller interface {
      CancelJob(jobID uuid.UUID) bool
  }
  ```
  The worker pool implements this interface. The service calls it for running jobs.

- **Option B**: Have the service just update the status, and the worker pool detects the cancellation.

Option A is cleaner. Update the service constructor to accept a `Canceller`.

#### 11.2 Cancel Logic by Status

**Pending jobs**: Just update status to `cancelled`. They haven't been picked up yet.

**Queued jobs**: Update status to `cancelled`. The worker will check the status before executing and skip it.
- Add a status check at the start of `processJob`: if job status is `cancelled`, skip.

**Running jobs**: Call `canceller.CancelJob(jobID)` to trigger the context cancellation. The executor should respond to `ctx.Done()` and return `context.Canceled`. The worker's `processJob` detects this and marks the job as `cancelled`.

#### 11.3 Executor Cancellation Patterns
Ensure all executors properly check for cancellation. The pattern:

```go
func (e *EmailExecutor) Execute(ctx context.Context, job *model.Job) error {
    // Phase 1: Parse
    // ...

    // Check cancellation before expensive work
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }

    // Phase 2: Do work
    // Use a timer instead of time.Sleep so cancellation works:
    select {
    case <-time.After(2 * time.Second):
        // work completed
    case <-ctx.Done():
        return ctx.Err()
    }

    return nil
}
```

The key insight: `time.Sleep()` is NOT cancellation-aware. Use `select` with `time.After` and `ctx.Done()` to make delays cancellable.

### Go Concepts
- **`context.WithCancel` chain**: When you cancel a parent context, all child contexts derived from it are also cancelled. The worker creates `jobCtx` from the server's `ctx`. Cancelling `jobCtx` stops only that job. Cancelling the server `ctx` stops all jobs (shutdown).
- **`ctx.Done()` channel pattern**: `ctx.Done()` returns a channel that is closed when the context is cancelled. Since receiving from a closed channel returns immediately, this provides a signaling mechanism. Use it in `select` to detect cancellation.
- **Cancellable sleep**: `time.Sleep()` blocks unconditionally. To make it cancellable:
  ```go
  select {
  case <-time.After(duration):
      // sleep completed normally
  case <-ctx.Done():
      // cancelled during sleep
      return ctx.Err()
  }
  ```
- **Interface segregation**: The service doesn't need the entire worker pool - it only needs the cancel capability. Defining a small `Canceller` interface follows the Interface Segregation Principle: depend on small, focused interfaces.
- **Thread-safe map access**: The `cancelFuncs` map is accessed from multiple goroutines (workers adding/removing, cancel endpoint reading). The `sync.Mutex` around all access prevents data races. Alternatively, `sync.Map` provides a lock-free concurrent map, but `sync.Mutex` with a regular map is usually simpler and faster for low-contention cases.

### Acceptance Criteria
- [ ] `POST /jobs/:id/cancel` on a pending job: status immediately becomes `cancelled`
- [ ] `POST /jobs/:id/cancel` on a running job: the executor stops within ~1 second
- [ ] After cancellation, the job status is `cancelled` in the database
- [ ] A cancelled job is NOT retried
- [ ] Cancelling an already-completed job returns an appropriate error (409 Conflict)
- [ ] Cancelling a non-existent job returns 404

---

## Step 12: Graceful Shutdown

### Goal
When the application receives a termination signal (Ctrl+C, SIGTERM), it should stop accepting new requests, stop the scheduler, wait for running jobs to complete (with a timeout), and close database connections - in that order.

### Tasks

#### 12.1 Signal Handling
Update `cmd/server/main.go`:

```go
// Create a context that is cancelled on SIGINT or SIGTERM
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
```

`signal.NotifyContext` creates a context that automatically cancels when the process receives one of the specified signals. This context gets passed down to all components.

#### 12.2 Server Shutdown
The HTTP server needs graceful shutdown:

```go
server := &http.Server{
    Addr:    ":" + cfg.ServerPort,
    Handler: router,
}

// Start server in a goroutine
go func() {
    if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        logger.Error("server error", "error", err)
    }
}()

// Wait for signal
<-ctx.Done()
logger.Info("shutting down...")

// Give the server a timeout to finish in-flight requests
shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
server.Shutdown(shutdownCtx)
```

`server.Shutdown()` stops accepting new connections and waits for in-flight requests to complete (up to the timeout).

#### 12.3 Shutdown Order
The shutdown sequence should be:

1. **Stop accepting new HTTP requests** - `server.Shutdown()`
2. **Stop the scheduler** - its context is cancelled (derived from the main context)
3. **Stop the worker pool** - close the queue, wait for in-flight jobs
4. **Close the database pool** - `pool.Close()` (deferred in main)

```go
// After server.Shutdown completes:
logger.Info("stopping scheduler...")
// scheduler stops because ctx is cancelled

logger.Info("stopping workers...")
workerPool.Stop() // closes queue, waits for wg

logger.Info("closing database...")
// pool.Close() runs via defer
```

#### 12.4 Worker Shutdown Timeout
Add a timeout to the worker pool's `Stop()` method to prevent hanging on a stuck job:

```go
func (p *WorkerPool) Stop() {
    p.queue.Close()

    done := make(chan struct{})
    go func() {
        p.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        p.logger.Info("all workers stopped")
    case <-time.After(30 * time.Second):
        p.logger.Warn("worker shutdown timed out, some jobs may not have finished")
    }
}
```

### Go Concepts
- **OS signals**: `SIGINT` is sent when you press Ctrl+C. `SIGTERM` is the standard "please stop" signal (sent by Docker, Kubernetes, systemd). `SIGKILL` cannot be caught - the OS terminates the process immediately.
- **`signal.NotifyContext`**: Combines signal handling with Go's context system. Returns a context that is cancelled when the signal arrives. This is cleaner than the older `signal.Notify` + channel pattern.
- **`http.Server.Shutdown`**: Gracefully shuts down the server: stops accepting new connections, waits for in-flight requests to complete, then returns. The timeout context prevents waiting forever if a request hangs.
- **`http.ErrServerClosed`**: Returned by `ListenAndServe` after `Shutdown` is called. This is expected behavior, not an error. Check for it to avoid logging a false alarm.
- **Shutdown ordering**: Resources should be shut down in reverse order of dependency. The server depends on the workers (it might create jobs), workers depend on the database. So: stop server first, then workers, then database. Think of it as unwinding a stack.
- **`select` with timeout**: The worker shutdown uses `select` between a completion signal and a timeout. This is a general pattern: "wait for X, but give up after Y time."

### Acceptance Criteria
- [ ] Ctrl+C triggers an orderly shutdown (visible in logs)
- [ ] In-flight HTTP requests complete before the server stops
- [ ] Running jobs finish before workers stop (up to timeout)
- [ ] Database connections are closed last
- [ ] No goroutine leaks (no "goroutine stuck" messages)
- [ ] Shutdown completes within a reasonable time (not hanging)

---

## Step 13: Testing

### Goal
Write tests that verify correctness at each layer: models, service logic, HTTP handlers, and worker behavior. Use the race detector to catch concurrency bugs.

### Tasks

#### 13.1 Model Tests
Create `internal/model/job_test.go`:

- Test `JobStatus.Valid()` with valid and invalid statuses
- Test `JobStatus.IsTerminal()` for each status
- Test `CreateJobRequest.Validate()`:
  - Valid request passes
  - Empty type fails
  - Invalid JSON payload fails
  - MaxRetries > 10 fails
  - ScheduledAt in the past fails

#### 13.2 Service Tests with Mock Repository
Create `internal/service/job_service_test.go`:

Create a mock repository that implements `JobRepository`:
```go
type mockRepo struct {
    jobs     map[uuid.UUID]*model.Job
    attempts map[uuid.UUID][]model.JobAttempt
}
```

Implement each interface method using the in-memory maps. This lets you test service logic without a database.

Test cases:
- CreateJob: valid input creates a pending job
- CreateJob: invalid input returns an error
- CancelJob: pending job is cancelled
- CancelJob: completed job returns an error
- RetryJob: failed job is reset to pending
- RetryJob: running job returns an error
- ValidTransition: all valid transitions pass, invalid ones fail

#### 13.3 Handler Tests
Create `internal/api/handler_test.go`:

Use `net/http/httptest` to test handlers without a real server:

```go
func TestCreateJob(t *testing.T) {
    // Setup
    service := ... // use mock repo
    handler := NewHandler(service, slog.Default())
    router := SetupRoutes(handler, slog.Default())

    // Create request
    body := `{"type":"email","payload":{"to":"test@example.com"}}`
    req := httptest.NewRequest("POST", "/jobs", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")

    // Record response
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    // Assert
    if w.Code != http.StatusCreated {
        t.Errorf("expected 201, got %d", w.Code)
    }
}
```

Test each endpoint for success and error cases:
- POST /jobs: 201 on success, 400 on bad input
- GET /jobs/:id: 200 on found, 404 on not found
- POST /jobs/:id/cancel: 200 on success, 409 on terminal state, 404 on not found
- GET /jobs: returns array, respects filters

#### 13.4 Worker Pool Tests
Create `internal/worker/pool_test.go`:

Create a test executor:
```go
type testExecutor struct {
    delay time.Duration
    err   error
}

func (e *testExecutor) Execute(ctx context.Context, job *model.Job) error {
    select {
    case <-time.After(e.delay):
        return e.err
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

Test cases:
- Job completes successfully: status is `success`
- Job fails: status is `failed`, error is recorded
- Job is cancelled: executor stops, status is `cancelled`
- Max retries reached: job is permanently failed
- Multiple concurrent jobs: all are processed (use WaitGroup or channel to synchronize)

#### 13.5 Integration Tests (Optional)
Create `internal/repository/postgres/job_repository_test.go`:

These tests need a real PostgreSQL database. Use build tags to separate them:
```go
//go:build integration

package postgres_test
```

Run with: `go test -tags=integration ./internal/repository/postgres/`

Test CRUD operations against the real database. Use a test database and clean up after each test with `t.Cleanup()`.

#### 13.6 Run with Race Detector
```bash
go test -race ./...
```

The race detector instruments your code to detect concurrent access to shared variables without proper synchronization. Fix any races it finds - they're real bugs, even if they don't cause visible problems yet.

### Go Concepts
- **Table-driven tests**: Go's testing convention for testing multiple cases:
  ```go
  tests := []struct {
      name   string
      input  string
      want   bool
  }{
      {"valid status", "pending", true},
      {"invalid status", "unknown", false},
  }
  for _, tt := range tests {
      t.Run(tt.name, func(t *testing.T) {
          got := model.JobStatus(tt.input).Valid()
          if got != tt.want {
              t.Errorf("got %v, want %v", got, tt.want)
          }
      })
  }
  ```
- **`httptest` package**: Creates in-memory HTTP servers and request recorders for testing handlers. `httptest.NewRequest()` creates a request, `httptest.NewRecorder()` captures the response. No real HTTP server needed.
- **Mock implementations**: Create a struct that implements an interface using in-memory data structures. This isolates the code under test from external dependencies (database, network). Mocks should be simple - just enough to make the test work.
- **`t.Run()` subtests**: Groups related test cases and gives each a name. Failed subtests are reported individually. You can run specific subtests: `go test -run TestCreateJob/valid_input`.
- **`-race` flag**: Go's built-in race detector. It instruments memory accesses at runtime and reports when two goroutines access the same variable concurrently without synchronization. Races are undefined behavior in Go - always fix them.
- **Build tags**: `//go:build integration` means this file is only compiled when `-tags=integration` is passed. Use this to separate slow tests (integration, database) from fast tests (unit).
- **`t.Cleanup()`**: Registers a function to run after the test (and any subtests) complete. Use it for teardown: deleting test data, closing connections. Runs even if the test panics.

### Acceptance Criteria
- [ ] `go test ./...` passes with no failures
- [ ] `go test -race ./...` reports no race conditions
- [ ] Model validation tests cover valid and invalid inputs
- [ ] Service tests verify business rules without a database
- [ ] Handler tests verify HTTP status codes and response bodies
- [ ] Worker tests verify successful execution, failure, cancellation, and retry

---

## Step 14: Polish

### Goal
Clean up the code, add developer documentation, and ensure the project is ready for others to understand and contribute to.

### Tasks

#### 14.1 Code Quality
Run linters and fix any issues:
```bash
go vet ./...
```

`go vet` catches common mistakes like unreachable code, incorrect format strings, and suspicious constructs.

Check formatting:
```bash
gofmt -d ./...
```

All Go code should be formatted with `gofmt`. Most editors do this automatically on save.

#### 14.2 Makefile
Create a `Makefile` at the project root for common commands:

```makefile
.PHONY: build run test lint migrate docker-up docker-down

build:
	go build -o bin/server ./cmd/server

run: docker-up
	go run ./cmd/server

test:
	go test -race ./...

test-integration:
	go test -race -tags=integration ./...

lint:
	go vet ./...

migrate: docker-up
	psql $(DATABASE_URL) -f migrations/001_create_jobs.sql
	psql $(DATABASE_URL) -f migrations/002_create_job_attempts.sql

docker-up:
	docker compose up -d

docker-down:
	docker compose down
```

#### 14.3 Example API Requests
Create `examples/requests.sh` or add curl examples to the README:

```bash
# Create a job
curl -X POST http://localhost:8080/jobs \
  -H "Content-Type: application/json" \
  -d '{"type":"email","payload":{"to":"user@example.com","subject":"Hello","body":"World"},"max_retries":3}'

# Get a job
curl http://localhost:8080/jobs/{job_id}

# List jobs
curl "http://localhost:8080/jobs?status=pending&limit=10"

# Cancel a job
curl -X POST http://localhost:8080/jobs/{job_id}/cancel

# Retry a failed job
curl -X POST http://localhost:8080/jobs/{job_id}/retry

# Get job history
curl http://localhost:8080/jobs/{job_id}/history
```

#### 14.4 README Updates
Update `README.md` with:
- Project description (2-3 sentences)
- Prerequisites (Go, Docker, psql)
- Quick start instructions (`make docker-up && make migrate && make run`)
- API documentation (endpoint table with method, path, description)
- Project structure overview (what each directory contains)
- How to run tests

#### 14.5 Final Review Checklist
- [ ] All files have correct package declarations
- [ ] No unused imports or variables
- [ ] Error messages are lowercase (Go convention) and descriptive
- [ ] No hardcoded values that should be configurable
- [ ] All goroutines can be stopped (no leaks)
- [ ] Database connections are properly closed
- [ ] Context is propagated through all layers
- [ ] Sensitive data (passwords, tokens) is never logged

### Go Concepts
- **`go vet`**: A static analysis tool that catches bugs the compiler doesn't. It checks for things like passing the wrong number of arguments to `Printf`, using `==` to compare a struct that contains a mutex (use `sync.Mutex` copies), and shadowed variables.
- **`gofmt`**: The standard Go formatter. Go has one canonical formatting style, enforced by this tool. This eliminates bikeshedding about style in code reviews. Run it or configure your editor to run it on save.
- **Go project layout conventions**: The `cmd/` directory holds entry points (main packages). `internal/` holds private packages that can't be imported by other modules. This is enforced by the Go compiler. Each subdirectory in `internal/` is a layer (model, repository, service, api, worker).
- **`.PHONY` in Makefile**: Declares that a target is not a file. Without this, if you accidentally create a file called `test`, `make test` would say "nothing to do" instead of running tests.

### Acceptance Criteria
- [ ] `go vet ./...` reports no issues
- [ ] `gofmt -d ./...` reports no formatting changes needed
- [ ] `go build ./...` compiles cleanly
- [ ] `make run` starts the application successfully
- [ ] All example API requests work against the running server
- [ ] README provides enough information for a new developer to get started

---

## Project Architecture Summary

```
cmd/
  server/
    main.go              # Entry point: wires dependencies, starts server

internal/
  config/
    config.go            # Environment configuration

  logger/
    logger.go            # Structured JSON logger

  model/
    job.go               # Job struct, JobStatus type
    job_attempt.go       # JobAttempt struct
    request.go           # API request/response types

  database/
    postgres.go          # Connection pool setup

  repository/
    job_repository.go    # Repository interface
    errors.go            # Sentinel errors
    postgres/
      job_repository.go  # PostgreSQL implementation

  service/
    job_service.go       # Business logic

  api/
    handler.go           # HTTP handlers
    routes.go            # Route registration
    middleware.go        # Request ID, logging, recovery
    response.go          # JSON response helpers

  queue/
    queue.go             # In-memory job queue (buffered channel)

  worker/
    pool.go              # Worker pool (goroutines consuming from queue)
    executor.go          # Executor interface
    executors/
      email.go           # Email job executor
      report.go          # Report job executor
      webhook.go         # Webhook job executor

  scheduler/
    scheduler.go         # Polls DB for scheduled/pending jobs

migrations/
  001_create_jobs.sql
  002_create_job_attempts.sql

docker-compose.yml
Makefile
.env
```

## Dependency Flow

```
main.go
  ├── config.Load()
  ├── logger.NewLogger()
  ├── database.Connect()
  ├── postgres.NewPostgresJobRepository(pool)
  ├── service.NewJobService(repo, logger)
  ├── worker.NewWorkerPool(queue, service, executors, logger)
  ├── scheduler.NewScheduler(service, queue, repo, logger)
  ├── api.NewHandler(service, logger)
  └── api.SetupRoutes(handler, logger)
```

Each layer depends only on the layer below it (or on interfaces). No circular dependencies. This makes each layer independently testable and replaceable.
