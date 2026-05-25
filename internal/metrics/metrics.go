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

	// QueueDepth tracks the live depth of each queue (high, medium, low, scheduled, dlq).
	// A non-zero dlq value means tasks have exhausted all retries and need manual intervention.
	QueueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "queue_depth_current",
			Help: "Current number of tasks in each queue",
		},
		[]string{"queue"},
	)
)

func Init() {
	prometheus.MustRegister(TasksProcessed)
	prometheus.MustRegister(TasksFailed)
	prometheus.MustRegister(TasksRetried)
	prometheus.MustRegister(TaskDuration)
	prometheus.MustRegister(QueueDepth)
}
