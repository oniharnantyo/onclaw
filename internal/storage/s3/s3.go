// Package s3 implements the S3-compatible object storage driver for the
// storage port (attachments design D15). Capability URLs stay proxied by
// onclaw for every driver — the bucket is never exposed and nothing presigns.
package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage"
)

func init() {
	storage.Register("s3", func(cfg storage.StorageConfig) (storage.Storage, error) {
		return New(cfg)
	})
}

const (
	// requestTimeout bounds each object operation (design D15).
	requestTimeout = 30 * time.Second
	// probeTimeout bounds the HeadBucket connectivity check the storage
	// settings pane runs on test-connection and save (design D16).
	probeTimeout = 5 * time.Second
	// maxObjectSize bounds in-memory reads for Open: storage.File requires
	// read/seek semantics and GetObject bodies are read-once, so the body is
	// buffered. Matches the largest attachment lane cap (drop: 50 MB, D4).
	maxObjectSize = 50 << 20
)

// driver implements storage.Storage on top of an S3-compatible bucket.
type driver struct {
	client *s3.Client
	bucket string
	// baseURL is the onclaw-proxied capability path served URLs carry (D15).
	baseURL string
}

// New creates a new S3-compatible storage driver using the provided configuration.
func New(cfg storage.StorageConfig) (storage.Storage, error) {
	d, err := newDriver(cfg)
	if err != nil {
		return nil, err
	}
	return d, nil
}

// newDriver builds the concrete driver; Probe needs the raw client and bucket.
func newDriver(cfg storage.StorageConfig) (*driver, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("%w: bucket is required for the s3 storage driver", domain.ErrInvalid)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("%w: region is required for the s3 storage driver", domain.ErrInvalid)
	}

	// Built from the passed configuration only: ambient environment or
	// shared-config credentials must not leak into a workspace-configured
	// backend whose values come from the sealed database row.
	awsCfg := aws.Config{
		Region:      cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		// Path-style addressing for MinIO/R2-class endpoints (design D15).
		o.UsePathStyle = cfg.UsePathStyle
	})

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "/api/v1/files/"
	}

	return &driver{
		client:  client,
		bucket:  cfg.Bucket,
		baseURL: baseURL,
	}, nil
}

// Probe runs a HeadBucket connectivity check against the configured backend
// with a short timeout (design D16: the storage pane's test connection and
// probe-gated save). The returned error's text carries the AWS failure
// reason for client-safe surface on 422.
func Probe(ctx context.Context, cfg storage.StorageConfig) error {
	d, err := newDriver(cfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	if _, err := d.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(d.bucket)}); err != nil {
		return fmt.Errorf("bucket %q unreachable: %w", d.bucket, err)
	}
	return nil
}

// Put stores object contents under the capability key.
func (d *driver) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if key == "" {
		return domain.ErrInvalid
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	if contentType == "" {
		contentType = "application/octet-stream"
	}
	input := &s3.PutObjectInput{
		Bucket:      aws.String(d.bucket),
		Key:         aws.String(key),
		Body:        r,
		ContentType: aws.String(contentType),
	}
	if size > 0 {
		input.ContentLength = aws.Int64(size)
	}
	if _, err := d.client.PutObject(ctx, input); err != nil {
		return fmt.Errorf("s3 put %q: %w", key, err)
	}
	return nil
}

// Open resolves a stored object by capability key. Missing or unknown keys
// return domain.ErrNotFound.
func (d *driver) Open(ctx context.Context, key string) (storage.File, error) {
	if key == "" {
		return nil, domain.ErrInvalid
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	out, err := d.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("s3 open %q: %w", key, err)
	}
	defer out.Body.Close()

	// GetObject bodies are read-once; buffer them (bounded by the attachment
	// size caps) to provide the port's read/seek semantics.
	data, err := io.ReadAll(io.LimitReader(out.Body, maxObjectSize+1))
	if err != nil {
		return nil, fmt.Errorf("s3 open %q: reading object body: %w", key, err)
	}
	if int64(len(data)) > maxObjectSize {
		return nil, fmt.Errorf("%w: s3 object %q exceeds the %d byte read cap", domain.ErrInvalid, key, maxObjectSize)
	}

	contentType := aws.ToString(out.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	size := int64(len(data))
	if out.ContentLength != nil && *out.ContentLength >= 0 {
		size = *out.ContentLength
	}

	return &memFile{
		Reader:      bytes.NewReader(data),
		contentType: contentType,
		size:        size,
	}, nil
}

// Delete removes a stored object. S3 deletes are idempotent — removing an
// absent key succeeds.
func (d *driver) Delete(ctx context.Context, key string) error {
	if key == "" {
		return domain.ErrInvalid
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	if _, err := d.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(key),
	}); err != nil && !isNotFound(err) {
		return fmt.Errorf("s3 delete %q: %w", key, err)
	}
	return nil
}

// URL generates the onclaw-proxied capability URL for a given key (design
// D15: capability URLs are driver-invariant; the bucket is never exposed).
func (d *driver) URL(key string) string {
	return d.baseURL + key
}

// isNotFound reports whether err is S3's missing-key failure — NoSuchKey, or
// a bare 404 from S3-compatible implementations that omit the error body.
func isNotFound(err error) bool {
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return true
	}
	var httpStatuser interface{ HTTPStatusCode() int }
	return errors.As(err, &httpStatuser) && httpStatuser.HTTPStatusCode() == http.StatusNotFound
}

// memFile is an in-memory storage.File over an S3 object body.
type memFile struct {
	*bytes.Reader
	contentType string
	size        int64
}

func (f *memFile) ContentType() string { return f.contentType }
func (f *memFile) Size() int64         { return f.size }
func (f *memFile) Close() error        { return nil }
