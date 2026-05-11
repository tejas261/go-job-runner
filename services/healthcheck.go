package services

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/go-job-runner/database"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"
)

var ErrUnsupportedJobType = errors.New("unsupported job_type")

type HealthcheckPayload struct {
	JobType        string    `json:"job_type"`
	URLList        []string  `json:"url_list"`
	ScheduledAt    time.Time `json:"scheduled_at"`
	CronExpression string    `json:"cron_expression"`
}

func HealthCheckService(ctx context.Context, db *pgxpool.Pool, payload HealthcheckPayload) (string, string, error) {
	if payload.JobType != "health-check" {
		return "", "failed", ErrUnsupportedJobType
	}

	jobPayloadJSON, err := json.Marshal(payload.URLList)
	if err != nil {
		return "", "failed", err
	}

	if payload.CronExpression != "" {
		_, err := PushJobToSchedulesTable(db, SchedulePayload{
			CronExpression: payload.CronExpression,
			JobPayload:     string(jobPayloadJSON),
			JobType:        payload.JobType,
		})
		return "", "Cron job created", err
	}

	jobRepo := database.NewRepository[any](db, "job")
	jobResultsRepo := database.NewRepository[any](db, "job_result")

	log.Printf("decoded job_type=%s url_count=%d", payload.JobType, len(payload.URLList))

	status := "pending"
	var scheduledAt any = nil
	if !payload.ScheduledAt.IsZero() && payload.ScheduledAt.After(time.Now()) {
		status = "scheduled"
		scheduledAt = payload.ScheduledAt
	}

	jobID, err := jobRepo.CreateAndReturnID(ctx,
		[]string{"type", "status", "scheduled_at"},
		[]any{payload.JobType, status, scheduledAt},
	)
	if err != nil {
		return "", "failed", err
	}

	err = jobResultsRepo.Create(ctx,
		[]string{"job_id", "job_payload", "result_data"},
		[]any{jobID, string(jobPayloadJSON), `{}`},
	)
	if err != nil {
		return "", "failed", err
	}

	return jobID, status, nil
}

type SchedulePayload struct {
	CronExpression string `json:"cron_expression,omitempty"`
	JobPayload     string `json:"job_payload,omitempty"`
	JobType        string `json:"job_type,omitempty"`
}

func PushJobToSchedulesTable(pool *pgxpool.Pool, payload SchedulePayload) (string, error) {
	scheduleRepo := database.NewRepository[any](pool, "schedule")
	ctx := context.Background()

	scheduleCron, err := cron.ParseStandard(payload.CronExpression)
	if err != nil {
		return "Error creating schedule", err
	}
	nextRun := scheduleCron.Next(time.Now())

	scheduleId, err := scheduleRepo.CreateAndReturnID(ctx,
		[]string{"job_type", "job_payload", "cron_expression", "next_run"},
		[]any{payload.JobType, payload.JobPayload, payload.CronExpression, nextRun},
	)
	if err != nil {
		return "Error creating schedule", err
	}

	return scheduleId, err
}
