package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	JobsProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "jobrunner_jobs_processed_total",
		Help: "Total number of jobs processed, labeled by job type and final status.",
	}, []string{"job_type", "status"})

	JobDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "jobrunner_job_duration_seconds",
		Help:    "Time taken to execute a job, in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"job_type"})

	NotificationsReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "jobrunner_notifications_received_total",
		Help: "Total number of pg_notify notifications received by the listener.",
	})
)

// RegisterQueueDepth exposes the current fill level of the job dispatch
// channel as a gauge. Must be called once, after the channel is created.
func RegisterQueueDepth(ch chan string) {
	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "jobrunner_queue_depth",
		Help: "Number of jobs currently buffered in the dispatch channel.",
	}, func() float64 {
		return float64(len(ch))
	})

	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "jobrunner_queue_capacity",
		Help: "Capacity of the dispatch channel buffer.",
	}, func() float64 {
		return float64(cap(ch))
	})
}
