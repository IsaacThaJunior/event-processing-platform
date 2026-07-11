package redisqueue_test

import (
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/isaacthajunior/mid-prod/queue"
	"github.com/isaacthajunior/mid-prod/queue/redisqueue"
)

// Example shows how an external consumer builds a Redis client and wraps it
// with redisqueue.NewRedisQueue to satisfy queue.Queue. It's not run as a
// doctest (no "Output:" comment) since it needs a live Redis server; it
// exists to prove, at compile time, that the package is usable from outside
// this module.
func Example() {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()

	var q queue.Queue = redisqueue.NewRedisQueue(client, "events_queue")

	if err := q.EnqueueWithPriority("task-123", "high"); err != nil {
		fmt.Println("enqueue failed:", err)
	}
}
