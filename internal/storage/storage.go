// Package storage defines the file/object storage port, driver registry, and capability key generation.
package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// File represents a stored file with read/seek capabilities and content metadata.
type File interface {
	io.ReadSeekCloser
	ContentType() string
	Size() int64
}

// Storage is the abstraction port for object/blob storage.
type Storage interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Open(ctx context.Context, key string) (File, error)
	Delete(ctx context.Context, key string) error
	URL(key string) string
}

// StorageConfig holds configuration for storage drivers.
type StorageConfig struct {
	Driver  string
	DataDir string
	BaseURL string
	// S3-compatible driver fields (attachments design D15). Ignored by the
	// local driver.
	Endpoint     string
	Region       string
	Bucket       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
}

// DriverOpenFunc creates a Storage driver instance from configuration.
type DriverOpenFunc func(cfg StorageConfig) (Storage, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]DriverOpenFunc)
)

// Register registers a storage driver by name. It panics if a driver with the same name is registered twice.
func Register(name string, open DriverOpenFunc) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("storage driver %q already registered", name))
	}
	registry[name] = open
}

// Open opens a storage instance using the named registered driver.
func Open(name string, cfg StorageConfig) (Storage, error) {
	registryMu.RLock()
	open, exists := registry[name]
	registryMu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("%w: unknown storage driver %q", domain.ErrInvalid, name)
	}
	return open(cfg)
}

// NewKey generates a cryptographically secure 128-bit random hex capability key (32 hex characters).
func NewKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("failed to generate random key: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
