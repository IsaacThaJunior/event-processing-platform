package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisQueue struct {
	client *redis.Client
	key    string
	ctx    context.Context
}

func NewRedisQueue(client *redis.Client, key string) *RedisQueue {
	return &RedisQueue{
		client: client,
		key:    key,
		ctx:    context.Background(),
	}
}

func priorityKey(priority string) string {
	switch priority {
	case "high":
		return "events_high"
	case "low":
		return "events_low"
	default:
		return "events_medium"
	}
}

func (r *RedisQueue) EnqueueWithPriority(taskID, priority string) error {
	key := priorityKey(priority)
	return r.client.LPush(r.ctx, key, taskID).Err()
}

func (r *RedisQueue) DequeuePriorityBlocking(timeout time.Duration) (string, string, error) {
	result, err := r.client.BLPop(
		r.ctx,
		timeout,
		"events_high",
		"events_medium",
		"events_low",
	).Result()

	if err == redis.Nil {
		return "", "", nil // timeout with no task
	}
	if err != nil {
		return "", "", fmt.Errorf("failed to dequeue: %w", err)
	}

	queueName := result[0]
	taskID := result[1]
	return taskID, queueName, nil
}

func (r *RedisQueue) EnqueueToDLQ(taskID string) error {
	return r.client.LPush(r.ctx, "dead_letter_queue", taskID).Err()
}

func (r *RedisQueue) Schedule(
	taskID, priority string,
	executeAt time.Time,
) error {
	score := float64(executeAt.Unix())

	return r.client.ZAdd(r.ctx, "scheduled_tasks", redis.Z{
		Score:  score,
		Member: fmt.Sprintf("%s|%s", priority, taskID),
	}).Err()
}

func (r *RedisQueue) PromoteScheduled() error {
	now := time.Now().Unix()

	tasks, err := r.client.ZRangeArgs(r.ctx, redis.ZRangeArgs{
		Key:     "scheduled_tasks",
		Start:   "-inf",
		Stop:    fmt.Sprintf("%d", now),
		ByScore: true,
	}).Result()
	if err != nil {
		return err
	}

	for _, item := range tasks {
		var priority, taskID string
		fmt.Sscanf(item, "%[^|]|%s", &priority, &taskID)

		if err := r.EnqueueWithPriority(taskID, priority); err != nil {
			return err
		}

		r.client.ZRem(r.ctx, "scheduled_tasks", item)
	}

	return nil
}

func (r *RedisQueue) Depth() (int64, error) {
	return r.client.LLen(r.ctx, r.key).Result()
}
