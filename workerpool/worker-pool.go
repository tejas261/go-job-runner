package workerpool

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/go-job-runner/configs"
	"github.com/go-job-runner/database"
	"github.com/go-job-runner/jobs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"
)

type jobNotification struct {
	ID          string          `json:"id"`
	JobType     string          `json:"job_type"`
	JobPayload  json.RawMessage `json:"job_payload,omitempty"`
	ScheduledAt *time.Time      `json:"scheduled_at,omitempty"`
}

func enqueueWhenDue(notification jobNotification, channel chan string) {
	payload, err := json.Marshal(notification)
	if err != nil {
		log.Printf("failed to marshal notification payload: %v", err)
		return
	}

	if notification.ScheduledAt == nil || !notification.ScheduledAt.After(time.Now()) {
		channel <- string(payload)
		return
	}

	delay := time.Until(*notification.ScheduledAt)
	fmt.Println("Job will be run on-->", *notification.ScheduledAt)
	time.AfterFunc(delay, func() {
		channel <- string(payload)
	})
}

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
		var job jobNotification
		if err := json.Unmarshal([]byte(notification.Payload), &job); err != nil {
			log.Printf("failed to parse notification payload: %v", err)
			continue
		}
		go enqueueWhenDue(job, channel)
	}
}

func processJobs(ctx context.Context, pool *pgxpool.Pool, jobCh <-chan string, workerID int) {
	jobRepo := database.NewRepository[any](pool, "job")

	for msg := range jobCh {
		var notification jobNotification
		if err := json.Unmarshal([]byte(msg), &notification); err != nil {
			log.Printf("failed to parse notification: %v", err)
			continue
		}

		payload := []byte(notification.JobPayload)

		jobID := notification.ID
		jobType := notification.JobType

		log.Printf("worker %d picked up job %s", workerID, jobID)

		if len(payload) == 0 || string(payload) == "null" {
			err := pool.QueryRow(ctx,
				"SELECT job_payload FROM job_result WHERE job_id = $1",
				jobID).Scan(&payload)
			if err != nil {
				log.Printf("failed to fetch job: %v", err)
				continue
			}
		}

		jobRepo.UpdateByID(ctx, jobID, []string{"status"}, []any{"processing"})

		// Call your handler
		runErr := jobs.Run(ctx, jobID, jobType, payload)

		if runErr != nil {
			jobRepo.UpdateByID(ctx, jobID,
				[]string{"status", "last_error"},
				[]any{"failed", runErr.Error()})
		} else {
			jobRepo.UpdateByID(ctx, jobID,
				[]string{"status"}, []any{"completed"})
		}
	}
}

func pollScheduledJobs(pool *pgxpool.Pool) {
	scheduleRepo := database.NewRepository[any](pool, "schedule")
	jobRepo := database.NewRepository[any](pool, "job")
	jobResultsRepo := database.NewRepository[any](pool, "job_result")
	ctx := context.Background()
	ticker := time.NewTicker(10 * time.Second)

	go func() {
		for t := range ticker.C {
			fmt.Println("Function executed at:", t)
			rows, err := scheduleRepo.FindRowsByColumnLTE(
				ctx,
				"next_run",
				time.Now(),
				[]string{"id", "job_type", "next_run", "job_payload", "cron_expression"},
			)
			if err != nil {
				log.Printf("failed to fetch due schedules: %v", err)
				continue
			}

			func() {
				defer rows.Close()
				for rows.Next() {
					var (
						scheduleID     string
						jobType        string
						nextRun        time.Time
						jobPayload     json.RawMessage
						cronExpression string
					)
					if scanErr := rows.Scan(&scheduleID, &jobType, &nextRun, &jobPayload, &cronExpression); scanErr != nil {
						log.Printf("failed to scan schedule row: %v", scanErr)
						continue
					}

					schedule, parseErr := cron.ParseStandard(cronExpression)
					if parseErr != nil {
						log.Printf("failed to parse cron for schedule %s: %v", scheduleID, parseErr)
						continue
					}
					nextScheduleRun := schedule.Next(time.Now())
					if updateErr := scheduleRepo.UpdateByID(ctx, scheduleID, []string{"next_run"}, []any{nextScheduleRun}); updateErr != nil {
						log.Printf("failed to update next_run for schedule %s: %v", scheduleID, updateErr)
					}

					jobID, createErr := jobRepo.CreateAndReturnID(ctx,
						[]string{"type", "status", "scheduled_at"},
						[]any{jobType, "pending", nextRun},
					)
					if createErr != nil {
						log.Printf("failed to create job from schedule %s: %v", scheduleID, createErr)
						continue
					}

					if createErr = jobResultsRepo.Create(ctx,
						[]string{"job_id", "job_payload", "result_data"},
						[]any{jobID, string(jobPayload), `{}`},
					); createErr != nil {
						log.Printf("failed to create job result for schedule %s job %s: %v", scheduleID, jobID, createErr)
						continue
					}

					log.Printf("created job %s from schedule %s, next run at %s", jobID, scheduleID, nextScheduleRun)

				}

				if rowsErr := rows.Err(); rowsErr != nil {
					log.Printf("error iterating schedules: %v", rowsErr)
				}
			}()
		}
	}()
}

func RunWorker() {
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

	jobRepo := database.NewRepository[any](pool, "job")
	rows, err := jobRepo.FindRowsByColumn(
		ctx,
		"status",
		"scheduled",
		[]string{"id", "type", "scheduled_at"},
	)
	if err != nil {
		log.Printf("failed to query scheduled jobs: %v", err)
	} else {
		defer rows.Close()

		for rows.Next() {
			var (
				id          string
				jobType     string
				scheduledAt *time.Time
			)

			if scanErr := rows.Scan(&id, &jobType, &scheduledAt); scanErr != nil {
				log.Printf("failed to scan scheduled job row: %v", scanErr)
				continue
			}

			if scheduledAt == nil {
				log.Printf("scheduled job id=%s type=%s scheduled_at=NULL", id, jobType)
				continue
			}

			enqueueWhenDue(jobNotification{
				ID:          id,
				JobType:     jobType,
				ScheduledAt: scheduledAt,
			}, notificationChannel)
		}

		if rowsErr := rows.Err(); rowsErr != nil {
			log.Printf("error while iterating scheduled jobs: %v", rowsErr)
		}
	}

	for i := range configs.Creds.WORKERS {
		go processJobs(ctx, pool, notificationChannel, i)
	}

	go pollScheduledJobs(pool)

	// Start listening to postgres
	listenToPostgres(context.Background(), pool, notificationChannel)

}
