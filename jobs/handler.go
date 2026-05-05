package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/go-job-runner/database"
)

type Handler interface {
	Type() string
	Run(ctx context.Context, jobID string, jobType string, payload []byte) error
}

func Run(ctx context.Context, jobID string, jobType string, payload []byte) error {
	switch jobType {
	case "health-check":
		var urls []string
		if err := json.Unmarshal(payload, &urls); err != nil {
			return err
		}
		results := make([]HealthCheckResult, len(urls))
		var wg sync.WaitGroup

		for idx, url := range urls {
			wg.Go(func() {
				results[idx] = PerformHealthCheck(url)
			})

		}
		wg.Wait()

		jobResultsRepo := database.NewRepository[any](database.DB, "job_results")

		fmt.Println("Storing this results -->", results)

		return jobResultsRepo.UpdateByColumn(context.Background(), "job_id", jobID, []string{"result_data"}, []any{results})
	}

	return fmt.Errorf("unknown job type: %s", jobType)
}
