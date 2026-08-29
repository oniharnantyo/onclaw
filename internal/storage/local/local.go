// Package local implements the local disk driver for the storage port.
package local

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage"
)

func init() {
	storage.Register("local", func(cfg storage.StorageConfig) (storage.Storage, error) {
		return New(cfg)
	})
}

type fileMeta struct {
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type localFile struct {
	*os.File
	contentType string
	size        int64
}

func (lf *localFile) ContentType() string {
	return lf.contentType
}

func (lf *localFile) Size() int64 {
	return lf.size
}

// driver implements storage.Storage on top of a local filesystem directory.
type driver struct {
	dataDir  string
	filesDir string
	baseURL  string
}

// New creates a new local storage driver using the provided configuration.
func New(cfg storage.StorageConfig) (storage.Storage, error) {
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = "./data"
	}

	filesDir := filepath.Join(dataDir, "files")
	if err := os.MkdirAll(filesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage files directory %q: %w", filesDir, err)
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "/api/v1/files/"
	}

	return &driver{
		dataDir:  dataDir,
		filesDir: filesDir,
		baseURL:  baseURL,
	}, nil
}

// Put writes file contents atomically using a temporary file and rename.
func (d *driver) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	targetPath, err := d.resolvePath(key)
	if err != nil {
		return err
	}

	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", targetDir, err)
	}

	// 1. Create temporary file in the same directory to ensure atomic rename
	tmpFile, err := os.CreateTemp(targetDir, ".tmp-put-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpFileName := tmpFile.Name()
	defer func() {
		// Clean up temp file if not renamed
		if _, err := os.Stat(tmpFileName); err == nil {
			_ = os.Remove(tmpFileName)
		}
	}()

	// 2. Stream data to temp file
	var written int64
	if size > 0 {
		written, err = io.Copy(tmpFile, io.LimitReader(r, size))
	} else {
		written, err = io.Copy(tmpFile, r)
	}
	if err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed writing file content: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed syncing file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed closing temp file: %w", err)
	}

	// 3. Write metadata file
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	meta := fileMeta{
		ContentType: contentType,
		Size:        written,
	}

	tmpMeta, err := os.CreateTemp(targetDir, ".tmp-meta-*")
	if err != nil {
		return fmt.Errorf("failed to create temp meta file: %w", err)
	}
	tmpMetaName := tmpMeta.Name()
	defer func() {
		if _, err := os.Stat(tmpMetaName); err == nil {
			_ = os.Remove(tmpMetaName)
		}
	}()

	if err := json.NewEncoder(tmpMeta).Encode(meta); err != nil {
		_ = tmpMeta.Close()
		return fmt.Errorf("failed encoding meta file: %w", err)
	}
	if err := tmpMeta.Sync(); err != nil {
		_ = tmpMeta.Close()
		return fmt.Errorf("failed syncing meta file: %w", err)
	}
	if err := tmpMeta.Close(); err != nil {
		return fmt.Errorf("failed closing temp meta file: %w", err)
	}

	// 4. Atomic renames
	metaPath := targetPath + ".meta"
	if err := os.Rename(tmpFileName, targetPath); err != nil {
		return fmt.Errorf("failed to rename content file: %w", err)
	}
	if err := os.Rename(tmpMetaName, metaPath); err != nil {
		_ = os.Remove(targetPath)
		return fmt.Errorf("failed to rename meta file: %w", err)
	}

	return nil
}

// Open opens a stored file by capability key.
func (d *driver) Open(ctx context.Context, key string) (storage.File, error) {
	targetPath, err := d.resolvePath(key)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	// Read metadata
	var contentType string
	var size int64

	metaPath := targetPath + ".meta"
	if metaFile, err := os.Open(metaPath); err == nil {
		var meta fileMeta
		if jsonErr := json.NewDecoder(metaFile).Decode(&meta); jsonErr == nil {
			contentType = meta.ContentType
			size = meta.Size
		}
		_ = metaFile.Close()
	}

	// Fallback to stat and sniffing if meta missing
	if contentType == "" || size == 0 {
		fi, statErr := f.Stat()
		if statErr != nil {
			_ = f.Close()
			return nil, statErr
		}
		size = fi.Size()

		buf := make([]byte, 512)
		n, _ := io.ReadFull(f, buf)
		if n > 0 {
			contentType = http.DetectContentType(buf[:n])
		} else {
			contentType = "application/octet-stream"
		}
		if _, seekErr := f.Seek(0, io.SeekStart); seekErr != nil {
			_ = f.Close()
			return nil, seekErr
		}
	}

	return &localFile{
		File:        f,
		contentType: contentType,
		size:        size,
	}, nil
}

// Delete removes a stored file and its metadata.
func (d *driver) Delete(ctx context.Context, key string) error {
	targetPath, err := d.resolvePath(key)
	if err != nil {
		return err
	}

	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(targetPath + ".meta")
	return nil
}

// URL generates the capability URL for a given key.
func (d *driver) URL(key string) string {
	return d.baseURL + key
}

// NewKey generates a new capability key (delegate to storage.NewKey).
func (d *driver) NewKey() (string, error) {
	return storage.NewKey()
}

func (d *driver) resolvePath(key string) (string, error) {
	if key == "" {
		return "", domain.ErrInvalid
	}

	clean := filepath.Clean(key)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("%w: invalid key %q", domain.ErrInvalid, key)
	}

	targetPath := filepath.Join(d.filesDir, clean)
	rel, err := filepath.Rel(d.filesDir, targetPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%w: key traverses out of storage dir %q", domain.ErrInvalid, key)
	}

	return targetPath, nil
}
