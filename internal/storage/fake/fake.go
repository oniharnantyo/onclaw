// Package fake provides an in-memory implementation of the Storage port for testing.
package fake

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage"
)

type fileEntry struct {
	data        []byte
	contentType string
	size        int64
}

type fakeFile struct {
	*bytes.Reader
	contentType string
	size        int64
}

func (f *fakeFile) Close() error {
	return nil
}

func (f *fakeFile) ContentType() string {
	return f.contentType
}

func (f *fakeFile) Size() int64 {
	return f.size
}

// fakeStorage is an in-memory fake storage implementation.
type fakeStorage struct {
	mu      sync.RWMutex
	files   map[string]fileEntry
	baseURL string
}

// New creates a new in-memory storage instance.
func New() storage.Storage {
	return &fakeStorage{
		files:   make(map[string]fileEntry),
		baseURL: "/api/v1/files/",
	}
}

// Put writes data into in-memory storage.
func (s *fakeStorage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if key == "" {
		return domain.ErrInvalid
	}

	var data []byte
	var err error
	if size > 0 {
		data, err = io.ReadAll(io.LimitReader(r, size))
	} else {
		data, err = io.ReadAll(r)
	}
	if err != nil {
		return err
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.files[key] = fileEntry{
		data:        data,
		contentType: contentType,
		size:        int64(len(data)),
	}
	return nil
}

// Open retrieves a file from in-memory storage.
func (s *fakeStorage) Open(ctx context.Context, key string) (storage.File, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, exists := s.files[key]
	if !exists {
		return nil, domain.ErrNotFound
	}

	return &fakeFile{
		Reader:      bytes.NewReader(entry.data),
		contentType: entry.contentType,
		size:        entry.size,
	}, nil
}

// Delete removes a file from in-memory storage.
func (s *fakeStorage) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.files, key)
	return nil
}

// URL generates the public URL for a given capability key.
func (s *fakeStorage) URL(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.baseURL + key
}

// SetBaseURL modifies the base URL used by URL(key).
func (s *fakeStorage) SetBaseURL(baseURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.baseURL = baseURL
}
