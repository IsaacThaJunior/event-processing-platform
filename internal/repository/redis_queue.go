package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisQueue struct {
	client *redis.Client
	key    string
	ctx    context.Context
}

type ScheduledTask struct {
	TaskID   string `json:"task_id"`
	Priority string `json:"priority"`
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

	payload, _ := json.Marshal(ScheduledTask{
		TaskID:   taskID,
		Priority: priority,
	})

	return r.client.ZAdd(r.ctx, "scheduled_tasks", redis.Z{
		Score:  score,
		Member: payload,
	}).Err()
}

func (r *RedisQueue) PromoteScheduled() error {
	nowStr := strconv.FormatInt(time.Now().Unix(), 10)

	tasks, err := r.client.ZRangeByScore(r.ctx, "scheduled_tasks", &redis.ZRangeBy{
		Min: "-inf",
		Max: nowStr,
	}).Result()
	if err != nil {
		return fmt.Errorf("failed to query scheduled tasks: %w", err)
	}
	if len(tasks) == 0 {
		return nil
	}

	pipe := r.client.Pipeline()
	for _, item := range tasks {
		var task ScheduledTask
		json.Unmarshal([]byte(item), &task)

		pipe.LPush(r.ctx, priorityKey(task.Priority), task.TaskID)
		pipe.ZRem(r.ctx, "scheduled_tasks", item)
	}

	if _, err := pipe.Exec(r.ctx); err != nil {
		return fmt.Errorf("failed to promote scheduled tasks: %w", err)
	}

	return nil
}

func (r *RedisQueue) GetDLQItems() ([]string, error) {
	items, err := r.client.LRange(r.ctx, "dead_letter_queue", 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list DLQ: %w", err)
	}
	return items, nil
}

func (r *RedisQueue) RemoveFromDLQ(taskID string) error {
	removed, err := r.client.LRem(r.ctx, "dead_letter_queue", 0, taskID).Result()
	if err != nil {
		return fmt.Errorf("failed to remove from DLQ: %w", err)
	}
	if removed == 0 {
		return fmt.Errorf("task %s not found in DLQ", taskID)
	}
	return nil
}

func (r *RedisQueue) Depth() (int64, error) {
	return r.client.LLen(r.ctx, r.key).Result()
}

func (r *RedisQueue) GetQueueDepths() (map[string]int64, error) {
	ctx := r.ctx
	pipe := r.client.Pipeline()

	highCmd := pipe.LLen(ctx, "events_high")
	medCmd := pipe.LLen(ctx, "events_medium")
	lowCmd := pipe.LLen(ctx, "events_low")
	schedCmd := pipe.ZCard(ctx, "scheduled_tasks")
	dlqCmd := pipe.LLen(ctx, "dead_letter_queue")

	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get queue depths: %w", err)
	}

	return map[string]int64{
		"high":      highCmd.Val(),
		"medium":    medCmd.Val(),
		"low":       lowCmd.Val(),
		"scheduled": schedCmd.Val(),
		"dlq":       dlqCmd.Val(),
	}, nil
}
