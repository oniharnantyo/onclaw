package api_test

import (
	"go/build"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchitectureInvariants(t *testing.T) {
	// Root directory of the repository (or workspace)
	// We want to verify that the service layer does not import net/http or database/sql
	t.Run("service package has no http or sql imports", func(t *testing.T) {
		pkg, err := build.Default.Import("github.com/oniharnantyo/onclaw/internal/api/service", "", build.ImportComment)
		if err != nil {
			t.Fatalf("failed to import service package: %v", err)
		}

		for _, imp := range pkg.Imports {
			if imp == "net/http" {
				t.Error("service package must not import net/http")
			}
			if imp == "database/sql" {
				t.Error("service package must not import database/sql")
			}
		}
	})

	t.Run("transport layer packages have no database/sql imports", func(t *testing.T) {
		// List of transport packages
		packages := []string{
			"github.com/oniharnantyo/onclaw/internal/api",
			"github.com/oniharnantyo/onclaw/internal/api/handler",
			"github.com/oniharnantyo/onclaw/internal/api/httpx",
			"github.com/oniharnantyo/onclaw/internal/api/auth",
		}

		for _, pkgPath := range packages {
			pkg, err := build.Default.Import(pkgPath, "", build.ImportComment)
			if err != nil {
				t.Fatalf("failed to import %s: %v", pkgPath, err)
			}

			for _, imp := range pkg.Imports {
				if imp == "database/sql" {
					t.Errorf("transport package %s must not import database/sql", pkgPath)
				}
				if strings.HasSuffix(imp, "internal/store/sqlite") {
					t.Errorf("transport package %s must not import sqlite implementation", pkgPath)
				}
			}
		}
	})

	t.Run("transport structs have no *sql.DB fields", func(t *testing.T) {
		// Verify server.go (api.Server struct) doesn't contain *sql.DB
		pkg, err := build.Default.Import("github.com/oniharnantyo/onclaw/internal/api", "", build.ImportComment)
		if err != nil {
			t.Fatalf("failed to import github.com/oniharnantyo/onclaw/internal/api: %v", err)
		}

		// Double check files inside internal/api for *sql.DB in server.go
		for _, file := range pkg.GoFiles {
			if filepath.Base(file) == "server.go" {
				// Read server.go and assert it doesn't contain "*sql.DB" or "sql.DB"
				srcDir := pkg.Dir
				path := filepath.Join(srcDir, file)
				data, err := os.Open(path)
				if err != nil {
					t.Fatalf("failed to open file %s: %v", path, err)
				}
				defer data.Close()

				contentBytes, err := io.ReadAll(data)
				if err != nil {
					t.Fatalf("failed to read file %s: %v", path, err)
				}

				content := string(contentBytes)
				if strings.Contains(content, "*sql.DB") || strings.Contains(content, "sql.DB") {
					t.Errorf("server.go contains forbidden sql.DB references")
				}
			}
		}
	})
}
