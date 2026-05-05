package services

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	"github.com/go-job-runner/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUnsupportedJobType = errors.New("unsupported job_type")

type HealthcheckPayload struct {
	JobType string   `json:"job_type"`
	URLList []string `json:"url_list"`
}

func HealthCheckService(ctx context.Context, db *pgxpool.Pool, payload HealthcheckPayload) (string, string, error) {
	if payload.JobType != "health-check" {
		return "", "failed", ErrUnsupportedJobType
	}

	log.Printf("decoded job_type=%s url_count=%d", payload.JobType, len(payload.URLList))

	jobRepo := database.NewRepository[any](db, "jobs")
	jobResultsRepo := database.NewRepository[any](db, "job_results")

	jobID, err := jobRepo.CreateAndReturnID(ctx,
		[]string{"type", "status"},
		[]any{payload.JobType, "pending"},
	)
	if err != nil {
		return "", "failed", err
	}

	emptyResultJSON := json.RawMessage(`{}`)

	err = jobResultsRepo.Create(ctx,
		[]string{"job_id", "job_payload", "result_data"},
		[]any{jobID, payload.URLList, emptyResultJSON},
	)
	if err != nil {
		return "", "failed", err
	}

	return jobID, "pending", nil
}
