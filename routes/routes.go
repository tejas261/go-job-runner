package routes

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-job-runner/database"
	"github.com/go-job-runner/services"
	"github.com/go-job-runner/utils"
)

type healthResponse struct {
	Success bool   `json:"success"`
	Status  string `json:"status"`
}

func HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_ = json.NewEncoder(w).Encode(healthResponse{
		Status:  "ok",
		Success: true,
	})
}

func CreateJob(w http.ResponseWriter, r *http.Request) {
	if database.DB == nil {
		http.Error(w, `{"error":"database not initialized"}`, http.StatusServiceUnavailable)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)

	utils.CheckNilError(err)

	var payload services.HealthcheckPayload
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	jobID, status, err := services.HealthCheckService(r.Context(), database.DB, payload)
	if err == services.ErrUnsupportedJobType {
		http.Error(w, `{"error":"unsupported job_type"}`, http.StatusBadRequest)
		return
	}
	if err != nil {
		log.Printf("failed to create job: %v", err)
		http.Error(w, `{"error":"failed to create job"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"status":  status,
		"job_id":  jobID,
	})
}

func GetJobStatusByID(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	status, err := services.GetJobStatus(r.Context(), database.DB, jobID)
	if err != nil {
		log.Printf("failed to get job status: %v", err)
		http.Error(w, `{"error":"failed to fetch job status"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthResponse{
		Status:  status,
		Success: true,
	})
}
