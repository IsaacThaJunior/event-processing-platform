package storage

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Client struct {
	mc     *minio.Client
	bucket string
}

func NewMinioClient() (*Client, error) {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	secretKey := os.Getenv("MINIO_SECRET_KEY")
	bucket := os.Getenv("MINIO_BUCKET")
	useSSL := os.Getenv("MINIO_USE_SSL") == "true"

	mc, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio: connect: %w", err)
	}

	return &Client{mc: mc, bucket: bucket}, nil
}

// EnsureBucket creates the bucket if it does not already exist.
func (c *Client) EnsureBucket(ctx context.Context) error {
	exists, err := c.mc.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("minio: check bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := c.mc.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("minio: create bucket: %w", err)
	}
	return nil
}

// Upload streams r into MinIO at the given object key and returns the key.
func (c *Client) Upload(ctx context.Context, key, contentType string, r io.Reader, size int64) (string, error) {
	_, err := c.mc.PutObject(ctx, c.bucket, key, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("minio: upload %s: %w", key, err)
	}
	return key, nil
}

// PresignedURL returns a time-limited URL the caller can use to download the object.
func (c *Client) PresignedURL(ctx context.Context, key string) (string, error) {
	url, err := c.mc.PresignedGetObject(ctx, c.bucket, key, 24*60*60*1e9, nil) // 24h
	if err != nil {
		return "", fmt.Errorf("minio: presign %s: %w", key, err)
	}
	return url.String(), nil
}
