package config_test

import (
	"os"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/config"
)

// writeDotEnv writes content to .env in the current working directory.
func writeDotEnv(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(".env", []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(".env") })
}

func TestLoadDotEnv(t *testing.T) {
	t.Run("missing file is not an error", func(t *testing.T) {
		t.Chdir(t.TempDir())

		if err := config.LoadDotEnv(); err != nil {
			t.Fatalf("LoadDotEnv() = %v, want nil for missing .env", err)
		}
	})

	t.Run("loads new variables", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeDotEnv(t, "DOTENV_TEST_NEW=from-file\n")
		defer os.Unsetenv("DOTENV_TEST_NEW")

		if err := config.LoadDotEnv(); err != nil {
			t.Fatalf("LoadDotEnv() = %v, want nil", err)
		}

		if got := os.Getenv("DOTENV_TEST_NEW"); got != "from-file" {
			t.Errorf("DOTENV_TEST_NEW = %q, want %q", got, "from-file")
		}
	})

	t.Run("does not override existing environment", func(t *testing.T) {
		t.Chdir(t.TempDir())
		t.Setenv("DOTENV_TEST_EXISTING", "from-env")
		writeDotEnv(t, "DOTENV_TEST_EXISTING=from-file\n")

		if err := config.LoadDotEnv(); err != nil {
			t.Fatalf("LoadDotEnv() = %v, want nil", err)
		}

		if got := os.Getenv("DOTENV_TEST_EXISTING"); got != "from-env" {
			t.Errorf("DOTENV_TEST_EXISTING = %q, want %q (file value must not override)", got, "from-env")
		}
	})

	t.Run("malformed file returns error", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeDotEnv(t, "this line has no equals sign\n")

		if err := config.LoadDotEnv(); err == nil {
			t.Fatal("LoadDotEnv() = nil, want error for malformed .env")
		}
	})

	t.Run("directory named .env is ignored", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if err := os.Mkdir(".env", 0o755); err != nil {
			t.Fatalf("failed to create .env directory: %v", err)
		}

		if err := config.LoadDotEnv(); err != nil {
			t.Fatalf("LoadDotEnv() = %v, want nil when .env is a directory", err)
		}
	})

	t.Run("standard dotenv syntax", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeDotEnv(t, "# comment line\n\nexport DOTENV_TEST_QUOTED=\"x y\"\nDOTENV_TEST_PLAIN=v\n")
		defer os.Unsetenv("DOTENV_TEST_QUOTED")
		defer os.Unsetenv("DOTENV_TEST_PLAIN")

		if err := config.LoadDotEnv(); err != nil {
			t.Fatalf("LoadDotEnv() = %v, want nil", err)
		}

		if got := os.Getenv("DOTENV_TEST_QUOTED"); got != "x y" {
			t.Errorf("DOTENV_TEST_QUOTED = %q, want %q", got, "x y")
		}
		if got := os.Getenv("DOTENV_TEST_PLAIN"); got != "v" {
			t.Errorf("DOTENV_TEST_PLAIN = %q, want %q", got, "v")
		}
	})
}
