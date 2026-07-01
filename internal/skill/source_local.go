package skill

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type localFetcher struct{}

// NewLocalFetcher creates a Fetcher for local directory paths.
func NewLocalFetcher() Fetcher {
	return &localFetcher{}
}

func (f *localFetcher) Fetch(ctx context.Context, source string, branch string) (string, error) {
	// Normalize path
	srcPath := source
	if !filepath.IsAbs(srcPath) {
		abs, err := filepath.Abs(srcPath)
		if err == nil {
			srcPath = abs
		}
	}

	fi, err := os.Stat(srcPath)
	if err != nil {
		return "", fmt.Errorf("local path does not exist: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "onclaw-skill-local")
	if err != nil {
		return "", err
	}

	if fi.IsDir() {
		err = copyDirVerbatim(srcPath, tempDir)
		if err != nil {
			os.RemoveAll(tempDir)
			return "", err
		}
	} else {
		// It's an archive file!
		isZip := strings.HasSuffix(strings.ToLower(srcPath), ".zip")
		isTarGz := strings.HasSuffix(strings.ToLower(srcPath), ".tar.gz") || strings.HasSuffix(strings.ToLower(srcPath), ".tgz")
		if !isZip && !isTarGz {
			os.RemoveAll(tempDir)
			return "", fmt.Errorf("unsupported local archive type (must be .zip, .tar.gz, or .tgz): %s", srcPath)
		}

		if isZip {
			err = extractZip(srcPath, tempDir)
			if err != nil {
				os.RemoveAll(tempDir)
				return "", fmt.Errorf("failed to extract zip: %w", err)
			}
		} else {
			file, err := os.Open(srcPath)
			if err != nil {
				os.RemoveAll(tempDir)
				return "", err
			}
			defer file.Close()
			err = extractTarGz(file, tempDir)
			if err != nil {
				os.RemoveAll(tempDir)
				return "", fmt.Errorf("failed to extract tar.gz: %w", err)
			}
		}
	}

	return tempDir, nil
}

func copyDirVerbatim(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}
