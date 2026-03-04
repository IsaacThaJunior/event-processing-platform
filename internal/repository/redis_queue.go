package repository

import (
	"context"
	"fmt"

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

func (r *RedisQueue) Enqueue(taskID string) error {
	return r.client.LPush(r.ctx, r.key, taskID).Err()
}

func (r *RedisQueue) Dequeue() (string, error) {
	result, err := r.client.RPop(r.ctx, r.key).Result()
	if err == redis.Nil {
		return "", nil // empty queue
	}
	if err != nil {
		return "", fmt.Errorf("failed to dequeue: %w", err)
	}
	return result, nil
}

func (r *RedisQueue) EnqueueToDLQ(taskID string) error {
	return r.client.LPush(context.Background(), "dead_letter_queue", taskID).Err()
}
