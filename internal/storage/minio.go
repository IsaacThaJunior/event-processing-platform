package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Client struct {
	mc       *minio.Client // internal endpoint — Upload, Download, EnsureBucket
	publicMC *minio.Client // public endpoint — PresignedGetObject (signature must match browser-facing host)
	bucket   string
}

func NewMinioClient() (*Client, error) {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	publicEndpoint := os.Getenv("MINIO_PUBLIC_ENDPOINT")
	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	secretKey := os.Getenv("MINIO_SECRET_KEY")
	bucket := os.Getenv("MINIO_BUCKET")
	useSSL := os.Getenv("MINIO_USE_SSL") == "true"

	if publicEndpoint == "" {
		publicEndpoint = endpoint
		slog.Warn("MINIO_PUBLIC_ENDPOINT not set — presigned URLs will use the internal Docker hostname and won't be reachable from a browser; set MINIO_PUBLIC_ENDPOINT=localhost:9000")
	}

	region := os.Getenv("MINIO_REGION")
	if region == "" {
		region = "us-east-1" // MinIO default
	}

	creds := credentials.NewStaticV4(accessKey, secretKey, "")

	mc, err := minio.New(endpoint, &minio.Options{Creds: creds, Secure: useSSL, Region: region})
	if err != nil {
		return nil, fmt.Errorf("minio: connect: %w", err)
	}

	// publicMC signs presigned URLs with the browser-facing hostname so the
	// AWS Signature V4 matches when the browser sends the download request.
	// Region must be set explicitly so the SDK does not call GetBucketLocation —
	// that call would go to localhost:9000 which is unreachable from inside Docker.
	publicMC := mc
	if publicEndpoint != endpoint {
		publicMC, err = minio.New(publicEndpoint, &minio.Options{Creds: creds, Secure: useSSL, Region: region})
		if err != nil {
			return nil, fmt.Errorf("minio: connect public client: %w", err)
		}
	}

	return &Client{
		mc:       mc,
		publicMC: publicMC,
		bucket:   bucket,
	}, nil
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

// Download returns the object body for the given key. Caller must close the returned ReadCloser.
func (c *Client) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("minio: download %s: %w", key, err)
	}
	return obj, nil
}

// PresignedURL returns a time-limited URL the caller can use to download the object.
// Uses publicMC so the AWS Signature V4 is computed against the browser-facing hostname
// from the start — replacing the host after signing breaks the signature.
func (c *Client) PresignedURL(ctx context.Context, key string) (string, error) {
	u, err := c.publicMC.PresignedGetObject(ctx, c.bucket, key, 24*60*60*1e9, nil) // 24h
	if err != nil {
		return "", fmt.Errorf("minio: presign %s: %w", key, err)
	}
	return u.String(), nil
}
