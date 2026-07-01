package skill

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type httpFetcher struct{}

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// NewHTTPFetcher creates a Fetcher for HTTP/HTTPS archive files.
func NewHTTPFetcher() Fetcher {
	return &httpFetcher{}
}

func (f *httpFetcher) Fetch(ctx context.Context, source string, branch string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", source, nil)
	if err != nil {
		return "", err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http GET failed with status: %s", resp.Status)
	}

	tempDir, err := os.MkdirTemp("", "onclaw-skill-http")
	if err != nil {
		return "", err
	}

	// We need to support both ZIP and Tar.Gz/tgz.
	// We read a preview of the body to detect the magic number or check URL suffix.
	isZip := strings.HasSuffix(strings.ToLower(source), ".zip")
	isTarGz := strings.HasSuffix(strings.ToLower(source), ".tar.gz") || strings.HasSuffix(strings.ToLower(source), ".tgz")

	if !isZip && !isTarGz {
		// Try content-type detection or default to tar.gz
		contentType := resp.Header.Get("Content-Type")
		if strings.Contains(contentType, "zip") && !strings.Contains(contentType, "gzip") {
			isZip = true
		} else {
			isTarGz = true // default fallback
		}
	}

	if isZip {
		// Download ZIP to a temp file, then extract it
		tempZip, err := os.CreateTemp("", "onclaw-download-*.zip")
		if err != nil {
			os.RemoveAll(tempDir)
			return "", err
		}
		defer os.Remove(tempZip.Name())
		defer tempZip.Close()

		if _, err := io.Copy(tempZip, resp.Body); err != nil {
			os.RemoveAll(tempDir)
			return "", err
		}

		err = extractZip(tempZip.Name(), tempDir)
		if err != nil {
			os.RemoveAll(tempDir)
			return "", fmt.Errorf("failed to extract zip: %w", err)
		}
	} else {
		// Extract tar.gz directly from response body
		err = extractTarGz(resp.Body, tempDir)
		if err != nil {
			os.RemoveAll(tempDir)
			return "", fmt.Errorf("failed to extract tar.gz: %w", err)
		}
	}

	return tempDir, nil
}

func extractTarGz(r io.Reader, dst string) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Normalize target path
		target := filepath.Join(dst, header.Name)
		// Clean the path to prevent directory traversal
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dst)) {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			// Ensure parent dir exists
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, header.FileInfo().Mode())
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}
	return nil
}

func extractZip(zipPath string, dst string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		target := filepath.Join(dst, f.Name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dst)) {
			continue
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
