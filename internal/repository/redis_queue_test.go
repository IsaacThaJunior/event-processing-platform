package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	testutil "github.com/isaacthajunior/mid-prod/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisQueue(t *testing.T) {
	client, cleanup := testutil.SetupTestRedis(t)
	defer cleanup()

	queue := NewRedisQueue(client, "test_queue")
	ctx := context.Background()

	t.Run("Enqueue and Dequeue", func(t *testing.T) {
		client.Del(ctx, "test_queue")

		err := queue.EnqueueWithPriority("task1", "high")
		require.NoError(t, err)

		err = queue.EnqueueWithPriority("task2", "low")
		require.NoError(t, err)

		task, _, err := queue.DequeuePriorityBlocking(5 * time.Second)
		require.NoError(t, err)
		assert.Equal(t, "task1", task)

		task, _, err = queue.DequeuePriorityBlocking(5 * time.Second)
		require.NoError(t, err)
		assert.Equal(t, "task2", task)
	})

	t.Run("Dequeue from empty queue", func(t *testing.T) {
		client.Del(ctx, "empty_queue")
		emptyQueue := NewRedisQueue(client, "empty_queue")

		task, _, err := emptyQueue.DequeuePriorityBlocking(5 * time.Second)
		require.NoError(t, err)
		assert.Equal(t, "", task)
	})

	t.Run("EnqueueToDLQ", func(t *testing.T) {
		client.Del(ctx, "dead_letter_queue")

		err := queue.EnqueueToDLQ("failed_task")
		require.NoError(t, err)

		length, err := client.LLen(ctx, "dead_letter_queue").Result()
		require.NoError(t, err)
		assert.Equal(t, int64(1), length)
	})

	t.Run("Depth returns correct count", func(t *testing.T) {
		queueKey := "depth_test"
		client.Del(ctx, queueKey)
		depthQueue := NewRedisQueue(client, queueKey) // Use the same key

		depth, err := depthQueue.Depth()
		require.NoError(t, err)
		assert.Equal(t, int64(0), depth)

		depthQueue.EnqueueWithPriority("task1", "high")
		depthQueue.EnqueueWithPriority("task2", "low")

		depth, err = depthQueue.Depth()
		require.NoError(t, err)
		assert.Equal(t, int64(2), depth)
	})

	t.Run("Concurrent operations", func(t *testing.T) {
		queueKey := "concurrent_queue"
		client.Del(ctx, queueKey)
		concurrentQueue := NewRedisQueue(client, queueKey) // Use the same key

		const numTasks = 100
		var wg sync.WaitGroup

		for i := range numTasks {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				err := concurrentQueue.EnqueueWithPriority(string(rune(id)), "medium")
				assert.NoError(t, err)
			}(i)
		}

		wg.Wait()

		length, err := concurrentQueue.Depth()
		require.NoError(t, err)
		assert.Equal(t, int64(numTasks), length)
	})

}
