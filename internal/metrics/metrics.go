package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	TasksProcessed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tasks_processed_total",
			Help: "Total processed tasks",
		},
		[]string{"type"},
	)

	TasksRetried = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tasks_retried_total",
			Help: "Total Retried Tasks",
		},
		[]string{"type"},
	)

	TasksFailed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tasks_failed_total",
			Help: "Total failed tasks",
		},
		[]string{"type"},
	)

	TaskDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "task_duration_seconds",
			Help:    "Task execution duration",
			Buckets: prometheus.DefBuckets,
		},
	)
)

func Init() {
	prometheus.MustRegister(TasksProcessed)
	prometheus.MustRegister(TasksFailed)
	prometheus.MustRegister(TasksRetried)
	prometheus.MustRegister(TaskDuration)
}
