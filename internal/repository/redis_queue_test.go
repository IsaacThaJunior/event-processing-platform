package repository

import (
	"context"
	"sync"
	"testing"

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

		err := queue.Enqueue("task1")
		require.NoError(t, err)

		err = queue.Enqueue("task2")
		require.NoError(t, err)

		task, err := queue.Dequeue()
		require.NoError(t, err)
		assert.Equal(t, "task1", task)

		task, err = queue.Dequeue()
		require.NoError(t, err)
		assert.Equal(t, "task2", task)
	})

	t.Run("Dequeue from empty queue", func(t *testing.T) {
		client.Del(ctx, "empty_queue")
		emptyQueue := NewRedisQueue(client, "empty_queue")

		task, err := emptyQueue.Dequeue()
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

		depthQueue.Enqueue("task1")
		depthQueue.Enqueue("task2")

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
				err := concurrentQueue.Enqueue(string(rune(id)))
				assert.NoError(t, err)
			}(i)
		}

		wg.Wait()

		length, err := concurrentQueue.Depth()
		require.NoError(t, err)
		assert.Equal(t, int64(numTasks), length)
	})

}
