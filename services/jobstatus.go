package services

import (
	"context"
	"database/sql"

	"github.com/go-job-runner/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func GetJobStatus(ctx context.Context, db *pgxpool.Pool, jobID string) (string, error) {
	jobRepo := database.NewRepository[any](db, "job")

	status, err := jobRepo.FindByID(ctx, jobID, func(row pgx.Row) (any, error) {
		var id string
		var jobType string
		var status string
		var scheduledAt sql.NullTime
		var attemptCount int
		var maxRetries int
		var lastError sql.NullString
		var createdAt sql.NullTime
		var updatedAt sql.NullTime

		err := row.Scan(
			&id,
			&jobType,
			&status,
			&scheduledAt,
			&attemptCount,
			&maxRetries,
			&lastError,
			&createdAt,
			&updatedAt,
		)
		if err != nil {
			return nil, err
		}
		return status, nil
	})
	if err != nil {
		return "", err
	}

	statusStr, ok := status.(string)
	if !ok {
		return "", pgx.ErrNoRows
	}
	return statusStr, nil
}
