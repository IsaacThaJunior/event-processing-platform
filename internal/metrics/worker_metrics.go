package metrics

import "github.com/isaacthajunior/pulse/worker"

// WorkerMetrics adapts this package's Prometheus vars to worker.Metrics.
type WorkerMetrics struct{}

var _ worker.Metrics = WorkerMetrics{}

func (WorkerMetrics) TaskProcessed(taskType string) {
	TasksProcessed.WithLabelValues(taskType).Inc()
}

func (WorkerMetrics) TaskRetried(taskType string) {
	TasksRetried.WithLabelValues(taskType).Inc()
}

func (WorkerMetrics) TaskFailed(taskType string) {
	TasksFailed.WithLabelValues(taskType).Inc()
}

func (WorkerMetrics) TaskDuration(seconds float64) {
	TaskDuration.Observe(seconds)
}

func (WorkerMetrics) QueueDepth(queueName string, depth int64) {
	QueueDepth.WithLabelValues(queueName).Set(float64(depth))
}
