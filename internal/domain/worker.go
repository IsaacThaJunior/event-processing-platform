package domain

// WorkerHealthStats is the snapshot returned by a worker pool's HealthStats call.
// Defined here (not in the worker package) so handler and worker can both use it
// without creating a circular import.
type WorkerHealthStats struct {
	TotalWorkers   int   `json:"total_workers"`
	ActiveWorkers  int32 `json:"active_workers"`
	IdleWorkers    int32 `json:"idle_workers"`
	TotalProcessed int64 `json:"total_processed"`
	TotalFailed    int64 `json:"total_failed"`
	UptimeSeconds  int64 `json:"uptime_seconds"`
}

type WorkerHealthProvider interface {
	HealthStats() WorkerHealthStats
}
