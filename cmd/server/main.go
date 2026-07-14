package main

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-job-runner/configs"
	"github.com/go-job-runner/database"
	"github.com/go-job-runner/routes"
	"github.com/go-job-runner/workerpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	envVars := configs.Load()
	database.Connect(envVars.DB_URL)
	defer database.Close()

	go workerpool.RunWorker()

	router := chi.NewRouter()

	router.Get("/health", routes.HealthCheck)
	router.Handle("/metrics", promhttp.Handler())
	router.Get("/job/{id}/status", routes.GetJobStatusByID)
	router.Post("/create-job", routes.CreateJob)

	addr := ":" + configs.Creds.PORT
	log.Printf("server running on PORT %s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("server failed: %v", err)
		return
	}
}
