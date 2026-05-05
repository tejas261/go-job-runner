package jobs

import (
	"log"
	"net/http"

	"github.com/go-job-runner/utils"
)

type HealthCheckResult struct {
	URL        string `json:"url"`
	Status     string `json:"status,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	Success    bool   `json:"success"`
	Response   string `json:"response,omitempty"`
	Error      string `json:"error,omitempty"`
}

func PerformHealthCheck(uri string) HealthCheckResult {
	normalizedURL := utils.EnsureURLScheme(uri)
	if normalizedURL == "" {
		log.Println("health check skipped: empty URL")
		return HealthCheckResult{
			URL:     uri,
			Success: false,
			Error:   "empty URL",
		}
	}

	resp, err := http.Get(normalizedURL)
	if err != nil {
		return HealthCheckResult{
			URL:     normalizedURL,
			Success: false,
			Error:   err.Error(),
		}
	}
	if resp == nil {
		return HealthCheckResult{
			URL:     normalizedURL,
			Success: false,
			Error:   "nil response",
		}
	}
	defer resp.Body.Close()

	log.Printf("response for URI %s -> %s", normalizedURL, resp.Status)
	log.Println("response", utils.MakeResponseReadable(resp))

	return HealthCheckResult{
		URL:        normalizedURL,
		Status:     resp.Status,
		StatusCode: resp.StatusCode,
		Success:    resp.StatusCode >= 200 && resp.StatusCode < 300,
		Response:   utils.MakeResponseReadable(resp),
	}
}
