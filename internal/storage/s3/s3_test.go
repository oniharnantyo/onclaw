package s3_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/storage/s3"
)

const testBucket = "onclaw-test-bucket"

// s3Stub is a minimal path-style S3 stand-in answering the four operations
// the driver issues (HeadBucket, PutObject, GetObject, DeleteObject). It
// never validates SigV4 signatures — only the wire shapes matter here.
type s3Stub struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
	// headStatus is the status code HeadBucket answers with (default 200).
	headStatus int
	// bare404 answers GetObject misses with a body-less 404, exercising the
	// no-error-body branch of the driver's missing-key mapping.
	bare404 bool
}

func newS3Stub() *s3Stub {
	return &s3Stub{
		objects:    make(map[string][]byte),
		types:      make(map[string]string),
		headStatus: http.StatusOK,
	}
}

func (s *s3Stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.WriteHeader(s.headStatus)
		return
	}

	// Path-style addressing: /{bucket}/{key...}
	prefix := "/" + testBucket + "/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, prefix)

	switch r.Method {
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if isAWSChunked(r.Header) {
			body = decodeAWSChunked(body)
		}
		s.mu.Lock()
		s.objects[key] = body
		s.types[key] = r.Header.Get("Content-Type")
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)

	case http.MethodGet:
		s.mu.Lock()
		data, ok := s.objects[key]
		ct := s.types[key]
		s.mu.Unlock()
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			if !s.bare404 {
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`))
			}
			return
		}
		if ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)

	case http.MethodDelete:
		s.mu.Lock()
		delete(s.objects, key)
		delete(s.types, key)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func isAWSChunked(h http.Header) bool {
	return strings.HasPrefix(h.Get("x-amz-content-sha256"), "STREAMING") ||
		strings.Contains(h.Get("Content-Encoding"), "aws-chunked")
}

// decodeAWSChunked strips the aws-chunked framing the SDK applies when
// streaming uploads carry trailing checksums. Chunk lines are hex sizes
// (optionally ;chunk-signature=...) followed by the data and a CRLF; the
// zero chunk ends the payload (trailing trailers are ignored).
func decodeAWSChunked(body []byte) []byte {
	var out []byte
	for {
		nl := bytes.IndexByte(body, '\n')
		if nl < 0 {
			return body // framing ended without a zero chunk — plain body
		}
		line := strings.TrimSpace(string(body[:nl]))
		sizeHex := line
		if i := strings.IndexByte(sizeHex, ';'); i >= 0 {
			sizeHex = sizeHex[:i]
		}
		size, err := strconv.ParseInt(sizeHex, 16, 64)
		if err != nil {
			return body // not a chunk size line — plain body
		}
		body = body[nl+1:]
		if size == 0 {
			return out
		}
		if int64(len(body)) < size {
			return append(out, body...) // truncated — best effort
		}
		out = append(out, body[:size]...)
		body = body[size:]
		body = bytes.TrimPrefix(body, []byte("\r\n"))
	}
}

// stubConfig points a path-style driver at the stub server.
func stubConfig(t *testing.T, serverURL string) storage.StorageConfig {
	t.Helper()
	return storage.StorageConfig{
		Driver:       "s3",
		Endpoint:     serverURL,
		Region:       "us-east-1",
		Bucket:       testBucket,
		AccessKey:    "test-access-key",
		SecretKey:    "test-secret-key",
		UsePathStyle: true,
	}
}

func TestS3NewValidation(t *testing.T) {
	if _, err := s3.New(storage.StorageConfig{Driver: "s3", Region: "us-east-1"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty bucket, got %v", err)
	}
	if _, err := s3.New(storage.StorageConfig{Driver: "s3", Bucket: testBucket}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty region, got %v", err)
	}
}

func TestS3RegistersUnderStorageRegistry(t *testing.T) {
	// The package init must make the driver reachable through the port
	// registry — this binary imports it exactly like internal/cli/drivers.go.
	s, err := storage.Open("s3", storage.StorageConfig{Driver: "s3", Region: "us-east-1", Bucket: testBucket})
	if err != nil || s == nil {
		t.Fatalf("expected the s3 driver in the storage registry, got err: %v", err)
	}
}

func TestS3URLIsProxiedCapabilityPath(t *testing.T) {
	withBase, err := s3.New(storage.StorageConfig{
		Driver:  "s3",
		Region:  "us-east-1",
		Bucket:  testBucket,
		BaseURL: "https://files.example.com/api/v1/files/",
	})
	if err != nil {
		t.Fatalf("unexpected New error: %v", err)
	}
	if got := withBase.URL("att/abc123"); got != "https://files.example.com/api/v1/files/att/abc123" {
		t.Fatalf("expected proxied capability URL with configured base, got %q", got)
	}
	// Default base matches the local driver — and never exposes the bucket.
	def, err := s3.New(storage.StorageConfig{Driver: "s3", Region: "us-east-1", Bucket: testBucket})
	if err != nil {
		t.Fatalf("unexpected New error: %v", err)
	}
	if got := def.URL("att/abc123"); got != "/api/v1/files/att/abc123" {
		t.Fatalf("expected default proxied capability path, got %q", got)
	}
}

func TestS3RoundTripAgainstStub(t *testing.T) {
	stub := newS3Stub()
	server := httptest.NewServer(stub)
	defer server.Close()

	drv, err := s3.New(stubConfig(t, server.URL))
	if err != nil {
		t.Fatalf("unexpected New error: %v", err)
	}
	ctx := context.Background()
	key := "att/roundtrip-key"
	content := []byte("attachment bytes \x00 with binary")

	// Open before put → domain.ErrNotFound (NoSuchKey body).
	if _, err := drv.Open(ctx, key); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing key, got %v", err)
	}

	// Put (content type set).
	if err := drv.Put(ctx, key, bytes.NewReader(content), int64(len(content)), "application/pdf"); err != nil {
		t.Fatalf("unexpected Put error: %v", err)
	}
	stub.mu.Lock()
	stored := stub.objects[key]
	stub.mu.Unlock()
	if !bytes.Equal(stored, content) {
		t.Fatalf("stub stored %d bytes, expected %d unframed bytes", len(stored), len(content))
	}

	// Open and verify metadata + read/seek semantics.
	f, err := drv.Open(ctx, key)
	if err != nil {
		t.Fatalf("unexpected Open error: %v", err)
	}
	defer f.Close()
	if f.ContentType() != "application/pdf" {
		t.Fatalf("expected content type application/pdf, got %q", f.ContentType())
	}
	if f.Size() != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), f.Size())
	}
	if _, err := f.Seek(16, io.SeekStart); err != nil {
		t.Fatalf("unexpected Seek error: %v", err)
	}
	tail, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("unexpected read-after-seek error: %v", err)
	}
	if !bytes.Equal(tail, content[16:]) {
		t.Fatalf("read after seek mismatch")
	}

	// Delete → object gone → domain.ErrNotFound.
	if err := drv.Delete(ctx, key); err != nil {
		t.Fatalf("unexpected Delete error: %v", err)
	}
	if _, err := drv.Open(ctx, key); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestS3OpenBare404NotFound(t *testing.T) {
	stub := newS3Stub()
	stub.bare404 = true
	server := httptest.NewServer(stub)
	defer server.Close()

	drv, err := s3.New(stubConfig(t, server.URL))
	if err != nil {
		t.Fatalf("unexpected New error: %v", err)
	}
	// S3-compatible implementations that omit the error body still map.
	if _, err := drv.Open(context.Background(), "att/nothing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for bare 404, got %v", err)
	}
}

func TestS3PutDefaultsContentType(t *testing.T) {
	stub := newS3Stub()
	server := httptest.NewServer(stub)
	defer server.Close()

	drv, err := s3.New(stubConfig(t, server.URL))
	if err != nil {
		t.Fatalf("unexpected New error: %v", err)
	}
	if err := drv.Put(context.Background(), "att/blob", strings.NewReader("x"), 1, ""); err != nil {
		t.Fatalf("unexpected Put error: %v", err)
	}
	if ct := stub.types["att/blob"]; ct != "application/octet-stream" {
		t.Fatalf("expected default content type application/octet-stream, got %q", ct)
	}
}

func TestProbeAgainstStub(t *testing.T) {
	stub := newS3Stub()
	server := httptest.NewServer(stub)
	defer server.Close()

	cfg := stubConfig(t, server.URL)
	if err := s3.Probe(context.Background(), cfg); err != nil {
		t.Fatalf("expected probe success, got %v", err)
	}

	// Failure: the error text carries the AWS failure reason for verbatim
	// surface on 422 (design D16).
	stub.mu.Lock()
	stub.headStatus = http.StatusForbidden
	stub.mu.Unlock()
	err := s3.Probe(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected probe failure on 403, got nil")
	}
	if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "HeadBucket") {
		t.Fatalf("expected probe error to carry the AWS failure reason, got %q", err.Error())
	}

	// Validation failures surface before any network call.
	if err := s3.Probe(context.Background(), storage.StorageConfig{Driver: "s3", Region: "us-east-1"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid probing without a bucket, got %v", err)
	}
}
