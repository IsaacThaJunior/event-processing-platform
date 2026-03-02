package domain

type Queue interface {
	Enqueue(taskID string) error
	Dequeue() (string, error)
}
