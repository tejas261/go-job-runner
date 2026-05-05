package workerpool

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/go-job-runner/configs"
	"github.com/go-job-runner/database"
	"github.com/go-job-runner/jobs"
	"github.com/jackc/pgx/v5/pgxpool"
)

func listenToPostgres(ctx context.Context, pool *pgxpool.Pool, channel chan string) {
	// 1. Acquire a dedicated connection from the pool
	conn, err := pool.Acquire(ctx)
	if err != nil {
		log.Fatalf("Failed to acquire connection: %v", err)
	}
	defer conn.Release() // Return connection to pool when function exits

	// 2. Issue LISTEN command
	_, err = conn.Conn().Exec(ctx, "LISTEN new_job")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	// 3. Wait for notifications in a loop

	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return // Context cancelled, stop listening
			}
			log.Printf("Error waiting for notification: %v", err)
			continue
		}
		fmt.Println("Received notification", notification)
		fmt.Printf("Received notification on channel '%s': %s\n", notification.Channel, notification.Payload)
		channel <- notification.Payload
	}
}

func processJobs(ctx context.Context, pool *pgxpool.Pool, jobCh <-chan string, workerID int) {
	jobRepo := database.NewRepository[any](pool, "jobs")

	for msg := range jobCh {
		var notification struct {
			ID      string `json:"id"`
			JobType string `json:"job_type"`
		}
		if err := json.Unmarshal([]byte(msg), &notification); err != nil {
			log.Printf("failed to parse notification: %v", err)
			continue
		}

		var payload []byte

		jobID := notification.ID
		jobType := notification.JobType

		log.Printf("worker %d picked up job %s", workerID, jobID)

		err := pool.QueryRow(ctx,
			"SELECT job_payload FROM job_results WHERE job_id = $1",
			jobID).Scan(&payload)
		if err != nil {
			log.Printf("failed to fetch job: %v", err)
			continue
		}

		jobRepo.UpdateByID(ctx, jobID, []string{"status"}, []any{"processing"})

		// Call your handler
		err = jobs.Run(ctx, jobID, jobType, payload)

		if err != nil {
			jobRepo.UpdateByID(ctx, jobID,
				[]string{"status", "last_error"},
				[]any{"failed", err.Error()})
		} else {
			jobRepo.UpdateByID(ctx, jobID,
				[]string{"status"}, []any{"completed"})
		}
	}
}

func RunWorker() {
	// Example setup
	pool, err := pgxpool.New(context.Background(), configs.Creds.DB_URL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	ctx := context.Background()
	notificationChannel := make(chan string, 100)

	if err := pool.Ping(context.Background()); err != nil {
		log.Fatal(err)
	}

	for i := range configs.Creds.WORKERS {
		go processJobs(ctx, pool, notificationChannel, i)
	}

	// Start listening
	listenToPostgres(context.Background(), pool, notificationChannel)

}
