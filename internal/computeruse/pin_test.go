package computeruse

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// The pin tests (add-computer-use tasks 2.3, design D1, spec computer-use
// "Driver acquisition and integrity"): a staged local archive drives
// ensureFromPin — a digest match installs and goes ready, a corrupted
// archive byte fails verification with no binary installed, a binary
// mismatch refuses to run, and a verified install short-circuits without
// re-downloading.

// stubDriverBody is the fake binary content the staged archives carry.
const stubDriverBody = "#!/bin/sh\nexit 0\n"

// stageArchive writes a tar.gz containing one file name=content and returns
// its path plus the SHA-256 of both layers.
func stageArchive(t *testing.T, dir, name, content string) (archivePath string, archiveSum, binarySum string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}))
	_, err := tw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	archivePath = filepath.Join(dir, "staged.tar.gz")
	require.NoError(t, os.WriteFile(archivePath, buf.Bytes(), 0o600))
	sum := sha256.Sum256(buf.Bytes())
	binSum := sha256.Sum256([]byte(content))
	return archivePath, hex.EncodeToString(sum[:]), hex.EncodeToString(binSum[:])
}

// stagedFetcher answers ensureFromPin's fetch seam: it copies the staged
// archive over whatever URL was named (the fetcher is untrusted — the
// digest check decides).
func stagedFetcher(path string) assetFetcher {
	return func(_ context.Context, _, destPath string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, 0o600)
	}
}

func testPin(archiveSum, binarySum string) pin {
	return pin{
		BaseURL: "https://example.invalid/releases/v9.9.9",
		Version: "9.9.9",
		Asset: pinAsset{
			ArchiveName:   "staged.tar.gz",
			ArchiveSHA256: archiveSum,
			Format:        "tar.gz",
			BinaryName:    "cua-driver",
			BinarySHA256:  binarySum,
		},
	}
}

// TestEnsureFromPinDigestMatchSucceeds: the happy first-enable path — the
// archive verifies, the binary installs executable and verifies, and the
// status is ready with the install path.
func TestEnsureFromPinDigestMatchSucceeds(t *testing.T) {
	root := t.TempDir()
	archivePath, archiveSum, binarySum := stageArchive(t, t.TempDir(), "cua-driver", stubDriverBody)

	status := ensureFromPin(context.Background(), testPin(archiveSum, binarySum), root, stagedFetcher(archivePath))
	require.True(t, status.Ready, "status: %+v", status)
	require.Empty(t, status.Reason)
	require.Equal(t, "9.9.9", status.Version)
	binaryPath := filepath.Join(root, "cua-driver", "9.9.9", "cua-driver")
	require.Equal(t, binaryPath, status.BinaryPath)
	info, err := os.Stat(binaryPath)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular())
}

// TestEnsureFromPinCorruptedArchiveFailsAndInstallsNothing: one corrupted
// byte fails the archive pin — the reason names the integrity failure, no
// binary exists (refuse-to-run), and the temp download is cleaned up.
func TestEnsureFromPinCorruptedArchiveFailsAndInstallsNothing(t *testing.T) {
	root := t.TempDir()
	staged := t.TempDir()
	archivePath, _, binarySum := stageArchive(t, staged, "cua-driver", stubDriverBody)

	// Corrupt one byte of the staged copy.
	corruptPath := filepath.Join(staged, "corrupt.tar.gz")
	data, err := os.ReadFile(archivePath)
	require.NoError(t, err)
	data[len(data)/2] ^= 0xFF
	require.NoError(t, os.WriteFile(corruptPath, data, 0o600))

	status := ensureFromPin(context.Background(), testPin("deadbeef", binarySum), root, stagedFetcher(corruptPath))
	require.False(t, status.Ready)
	require.Contains(t, status.Reason, "failed its SHA-256 pin")
	require.Contains(t, status.Reason, "staged.tar.gz")
	_, statErr := os.Stat(filepath.Join(root, "cua-driver", "9.9.9", "cua-driver"))
	require.True(t, os.IsNotExist(statErr), "no binary may be installed on an archive mismatch")
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		require.False(t, bytes.HasPrefix([]byte(e.Name()), []byte(".cua-download-")), "temp downloads are cleaned up")
	}
}

// TestEnsureFromPinBinaryMismatchRefusesToRun: an archive that verifies but
// carries a different binary fails the binary pin — the bad binary is
// removed so a later enable can re-download, and the status refuses.
func TestEnsureFromPinBinaryMismatchRefusesToRun(t *testing.T) {
	root := t.TempDir()
	staged := t.TempDir()
	archivePath, archiveSum, _ := stageArchive(t, staged, "cua-driver", stubDriverBody)

	status := ensureFromPin(context.Background(), testPin(archiveSum, "feedface"), root, stagedFetcher(archivePath))
	require.False(t, status.Ready)
	require.Contains(t, status.Reason, "cua-driver failed its SHA-256 pin")
	_, statErr := os.Stat(filepath.Join(root, "cua-driver", "9.9.9", "cua-driver"))
	require.True(t, os.IsNotExist(statErr), "the mismatched binary is removed, never left in place")
}

// TestEnsureFromPinVerifiedInstallShortCircuits: a second call against a
// verified install answers ready without touching the network.
func TestEnsureFromPinVerifiedInstallShortCircuits(t *testing.T) {
	root := t.TempDir()
	staged := t.TempDir()
	archivePath, archiveSum, binarySum := stageArchive(t, staged, "cua-driver", stubDriverBody)

	first := ensureFromPin(context.Background(), testPin(archiveSum, binarySum), root, stagedFetcher(archivePath))
	require.True(t, first.Ready)

	calls := 0
	status := ensureFromPin(context.Background(), testPin(archiveSum, binarySum), root, func(context.Context, string, string) error {
		calls++
		return nil
	})
	require.True(t, status.Ready)
	require.Equal(t, first.BinaryPath, status.BinaryPath)
	require.Zero(t, calls, "a verified install must not re-download")
}

// TestEnsureFromPinTamperedInstallRefusesWithoutRepair: a binary present but
// mismatched refuses to run and does not silently re-download (the spec's
// refuse-to-run; re-enable is the recovery path).
func TestEnsureFromPinTamperedInstallRefusesWithoutRepair(t *testing.T) {
	root := t.TempDir()
	staged := t.TempDir()
	archivePath, archiveSum, binarySum := stageArchive(t, staged, "cua-driver", stubDriverBody)

	first := ensureFromPin(context.Background(), testPin(archiveSum, binarySum), root, stagedFetcher(archivePath))
	require.True(t, first.Ready)

	// Tamper with the installed binary.
	binaryPath := first.BinaryPath
	require.NoError(t, os.WriteFile(binaryPath, []byte("tampered"), 0o755))

	calls := 0
	status := ensureFromPin(context.Background(), testPin(archiveSum, binarySum), root, func(context.Context, string, string) error {
		calls++
		return nil
	})
	require.False(t, status.Ready)
	require.Contains(t, status.Reason, "failed its SHA-256 pin")
	require.Zero(t, calls, "a tampered install refuses instead of silently re-downloading")
}

// TestEnsureFromPinMissingBinaryRedownloads: an install dir without its
// binary (deleted after a prior enable) re-downloads on the next call.
func TestEnsureFromPinMissingBinaryRedownloads(t *testing.T) {
	root := t.TempDir()
	staged := t.TempDir()
	archivePath, archiveSum, binarySum := stageArchive(t, staged, "cua-driver", stubDriverBody)

	first := ensureFromPin(context.Background(), testPin(archiveSum, binarySum), root, stagedFetcher(archivePath))
	require.True(t, first.Ready)
	require.NoError(t, os.Remove(first.BinaryPath))

	status := ensureFromPin(context.Background(), testPin(archiveSum, binarySum), root, stagedFetcher(archivePath))
	require.True(t, status.Ready, "status: %+v", status)
	require.Equal(t, first.BinaryPath, status.BinaryPath)
}

// TestEnsureFromPinDownloadFailureSurfaces: a fetch error is a readable
// not-ready reason, never a panic and never a partial install.
func TestEnsureFromPinDownloadFailureSurfaces(t *testing.T) {
	root := t.TempDir()
	status := ensureFromPin(context.Background(), testPin("aa", "bb"), root, func(context.Context, string, string) error {
		return os.ErrPermission
	})
	require.False(t, status.Ready)
	require.Contains(t, status.Reason, "download driver release")
}

// TestDriversRootEnvOverride pins the test/dev override seam.
func TestDriversRootEnvOverride(t *testing.T) {
	t.Setenv(EnvDriversRoot, "/custom/drivers")
	require.Equal(t, "/custom/drivers", DriversRoot())
	t.Setenv(EnvDriversRoot, "  ")
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".buddies", "drivers"), DriversRoot())
}

// TestEnsureDriverDownloadedRefusesTamperedInstall drives the public entry
// point offline: a seeded install whose binary fails its pin is refused —
// refuse-to-run with the integrity reason, no launch, no re-download.
func TestEnsureDriverDownloadedRefusesTamperedInstall(t *testing.T) {
	root := t.TempDir()
	binaryName := "cua-driver"
	if runtime.GOOS == "windows" {
		binaryName = "cua-driver.exe"
	}
	installDir := filepath.Join(root, "cua-driver", pinnedVersion)
	require.NoError(t, os.MkdirAll(installDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(installDir, binaryName), []byte("tampered"), 0o755))

	status := EnsureDriverDownloaded(context.Background(), root)
	require.False(t, status.Ready)
	require.Equal(t, pinnedVersion, status.Version)
	require.Contains(t, status.Reason, "failed its SHA-256 pin")
	_, err := os.Stat(filepath.Join(installDir, binaryName))
	require.True(t, os.IsNotExist(err), "the mismatched binary is removed")
}
