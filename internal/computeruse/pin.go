package computeruse

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The vendored driver pin (add-computer-use design D1, spec computer-use
// "Driver acquisition and integrity"): the MIT cua-driver binary from
// trycua/cua, digest-pinned per release. On first enable the pinned release
// archive is downloaded into <drivers root>/cua-driver/<version>/, the
// archive and the launch binary are verified against their SHA-256 pins, and
// a mismatch refuses to run the driver — no launch, an integrity failure
// surfaced to the pane, and no computer-use tools. The vendor's code
// signature is preserved: files are extracted and chmod'ed, never re-signed.
//
// The pins below are the real cua-driver-rs-v0.41.2 release digests,
// cross-verified against the release's checksums.txt and the GitHub API's
// per-asset digests (archive), and computed from the extracted archives
// (binary). darwin ships one universal (x86_64+arm64) binary. A pin bump
// re-walks this whole verification chain — see notes.md 7.3.

// pinnedVersion is the pinned cua-driver release version; pinnedTag is the
// GitHub release tag it is downloaded from.
const (
	pinnedVersion = "0.41.2"
	pinnedTag     = "cua-driver-rs-v0.41.2"
)

// pinnedReleaseBaseURL is the release's download base: one pinned asset per
// desktop platform hangs off it.
const pinnedReleaseBaseURL = "https://github.com/trycua/cua/releases/download/" + pinnedTag

// pinAsset is one platform's pinned release archive: the archive's name,
// format, and SHA-256, plus the launch binary's name inside the archive and
// its own SHA-256 (defense in depth — the binary is re-verified before every
// ready answer, so a tampered install never runs).
type pinAsset struct {
	ArchiveName   string
	ArchiveSHA256 string
	Format        string // "tar.gz" or "zip"
	BinaryName    string
	BinarySHA256  string
}

// pinnedAssets maps GOOS/GOARCH to the pinned archive. darwin ships the
// universal binary for both architectures; windows and linux have per-arch
// assets (the driver's Windows UIA helper rides inside the windows archive;
// the linux Wayland helper ships in the same archive).
var pinnedAssets = map[string]pinAsset{
	"darwin/amd64": {
		ArchiveName:   "cua-driver-rs-" + pinnedVersion + "-darwin-universal-binary.tar.gz",
		ArchiveSHA256: "4581e39357df39c166e7b13336ae785fafd0b289fbda7600025833262fef5a31",
		Format:        "tar.gz",
		BinaryName:    "cua-driver",
		BinarySHA256:  "a39a7faf8ec34354db693684ce504e4d802d363fdf8a69b9014a28d177974d4f",
	},
	"darwin/arm64": {
		ArchiveName:   "cua-driver-rs-" + pinnedVersion + "-darwin-universal-binary.tar.gz",
		ArchiveSHA256: "4581e39357df39c166e7b13336ae785fafd0b289fbda7600025833262fef5a31",
		Format:        "tar.gz",
		BinaryName:    "cua-driver",
		BinarySHA256:  "a39a7faf8ec34354db693684ce504e4d802d363fdf8a69b9014a28d177974d4f",
	},
	"windows/amd64": {
		ArchiveName:   "cua-driver-rs-" + pinnedVersion + "-windows-x86_64-binary.zip",
		ArchiveSHA256: "1084e1f0e74ce3d281a7010c7d765c3039c05dbc503fc0470f4b95751b58c391",
		Format:        "zip",
		BinaryName:    "cua-driver.exe",
		BinarySHA256:  "a837d3c6272ad3b2efe9f8db2c5a50b180de6c450f6fa7d8106f6e8575ee2384",
	},
	"windows/arm64": {
		ArchiveName:   "cua-driver-rs-" + pinnedVersion + "-windows-arm64-binary.zip",
		ArchiveSHA256: "95c0a279997ef4a2ac581d951ff135c79432e2681c5c24aa1492938902cdad2f",
		Format:        "zip",
		BinaryName:    "cua-driver.exe",
		BinarySHA256:  "cd6a2361dafc5d2c91f854f0905bbffa96cf664de664fb343f6f83fba468e768",
	},
	"linux/amd64": {
		ArchiveName:   "cua-driver-rs-" + pinnedVersion + "-linux-x86_64-binary.tar.gz",
		ArchiveSHA256: "aab4bc0978822102bf8276350aa2c101d3f7eb519adbc17fa949b5ea1bac892d",
		Format:        "tar.gz",
		BinaryName:    "cua-driver",
		BinarySHA256:  "cfecbb65595dea1d99832219030924e9777deb885d8468d26fb1b198b6532d4c",
	},
	"linux/arm64": {
		ArchiveName:   "cua-driver-rs-" + pinnedVersion + "-linux-arm64-binary.tar.gz",
		ArchiveSHA256: "4f2358c6b101844a4dc9556545cd0480e701d30dff90bd2b3f28045b8d6b392b",
		Format:        "tar.gz",
		BinaryName:    "cua-driver",
		BinarySHA256:  "3be5d3127fdd78799ed140020385c795c6f677c026ddf7adb244ca686142cd00",
	},
}

// EnvDriversRoot overrides the drivers root directory (tests and custom
// layouts), mirroring the browseruse env-override pattern. Empty uses the
// canonical ~/.buddies/drivers.
const EnvDriversRoot = "BUDDIES_COMPUTER_DRIVERS_ROOT"

// DriversRoot resolves the drivers root directory the pinned driver installs
// under (design D1: ~/.buddies/drivers).
func DriversRoot() string {
	if root := strings.TrimSpace(os.Getenv(EnvDriversRoot)); root != "" {
		return root
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".buddies", "drivers")
}

// PinStatus is the pinned driver's readiness view: simple and
// self-describing by contract — the settings service renders its own types
// around these fields, so nothing here is more than scalars. Ready is true
// exactly when the verified launch binary is in place; Reason is empty when
// Ready and a readable integrity/availability answer otherwise.
type PinStatus struct {
	// Version is the pinned release this status describes (always set).
	Version string `json:"version"`
	// Ready reports the verified binary's presence.
	Ready bool `json:"ready"`
	// BinaryPath is the verified launch binary's path when Ready.
	BinaryPath string `json:"binaryPath,omitempty"`
	// Reason explains a not-ready state: never the digest itself, always the
	// failure class (download failed, archive mismatch, binary mismatch).
	Reason string `json:"reason,omitempty"`
}

// assetFetcher downloads one release asset to destPath. Production streams
// over HTTP; tests stage a local file. The digest check happens on the
// fetched bytes, never on trust in the fetcher.
type assetFetcher func(ctx context.Context, assetURL, destPath string) error

// EnsureDriverDownloaded makes the pinned driver present and verified under
// driversRoot (design D1's download-on-first-enable). Idempotent: a verified
// install short-circuits without a download; a missing binary re-downloads;
// a present-but-mismatched binary refuses to run without a silent repair.
func EnsureDriverDownloaded(ctx context.Context, driversRoot string) PinStatus {
	asset, ok := pinnedAssets[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return PinStatus{Version: pinnedVersion, Reason: "computer use has no pinned driver build for this platform (" + runtime.GOOS + "/" + runtime.GOARCH + ")"}
	}
	return ensureFromPin(ctx, pin{
		BaseURL: pinnedReleaseBaseURL,
		Version: pinnedVersion,
		Asset:   asset,
	}, driversRoot, httpFetch)
}

// pin is one pinned release: the values ensureFromPin verifies against.
type pin struct {
	BaseURL string
	Version string
	Asset   pinAsset
}

// ensureFromPin is the verification core the tests drive with staged
// archives: install dir <driversRoot>/cua-driver/<version>/, digest-pinned
// archive and binary, refuse-to-run on any mismatch.
func ensureFromPin(ctx context.Context, p pin, driversRoot string, fetch assetFetcher) PinStatus {
	if driversRoot == "" {
		return PinStatus{Version: p.Version, Reason: "no drivers root configured"}
	}
	versionDir := filepath.Join(driversRoot, "cua-driver", p.Version)
	binaryPath := filepath.Join(versionDir, p.Asset.BinaryName)

	// A present binary must match its pin before anything runs (spec
	// computer-use "Digest mismatch refuses to run"): the mismatched binary
	// is removed so the next enable re-downloads instead of re-refusing.
	if info, err := os.Stat(binaryPath); err == nil && info.Mode().IsRegular() {
		if err := verifyFileDigest(binaryPath, p.Asset.BinaryName, p.Asset.BinarySHA256); err != nil {
			_ = os.Remove(binaryPath)
			return PinStatus{Version: p.Version, Reason: err.Error()}
		}
		return PinStatus{Version: p.Version, Ready: true, BinaryPath: binaryPath}
	}

	if err := os.MkdirAll(driversRoot, 0o700); err != nil {
		return PinStatus{Version: p.Version, Reason: fmt.Sprintf("create drivers root: %v", err)}
	}

	// Download to a temp file on the same volume, verify the archive pin,
	// then extract — a corrupted download never reaches the install dir.
	tmp, err := os.CreateTemp(driversRoot, ".cua-download-*")
	if err != nil {
		return PinStatus{Version: p.Version, Reason: fmt.Sprintf("stage download: %v", err)}
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed away or already removed
	if err := fetch(ctx, p.BaseURL+"/"+p.Asset.ArchiveName, tmpPath); err != nil {
		return PinStatus{Version: p.Version, Reason: fmt.Sprintf("download driver release: %v", err)}
	}
	if err := verifyFileDigest(tmpPath, p.Asset.ArchiveName, p.Asset.ArchiveSHA256); err != nil {
		return PinStatus{Version: p.Version, Reason: err.Error()}
	}

	if err := os.MkdirAll(versionDir, 0o700); err != nil {
		return PinStatus{Version: p.Version, Reason: fmt.Sprintf("create install dir: %v", err)}
	}
	if err := extractArchive(tmpPath, p.Asset.Format, versionDir); err != nil {
		return PinStatus{Version: p.Version, Reason: fmt.Sprintf("extract driver release: %v", err)}
	}
	if err := os.Chmod(binaryPath, 0o755); err != nil {
		return PinStatus{Version: p.Version, Reason: fmt.Sprintf("mark driver executable: %v", err)}
	}
	if err := verifyFileDigest(binaryPath, p.Asset.BinaryName, p.Asset.BinarySHA256); err != nil {
		// Refuse to run: remove the bad binary so a later enable re-downloads
		// instead of re-refusing, and report the integrity failure.
		_ = os.Remove(binaryPath)
		return PinStatus{Version: p.Version, Reason: err.Error()}
	}
	return PinStatus{Version: p.Version, Ready: true, BinaryPath: binaryPath}
}

// httpFetch is the production asset fetcher: a context-bounded GET streamed
// to destPath (GitHub release downloads redirect; the default client
// follows).
func httpFetch(ctx context.Context, assetURL, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %s", resp.Status)
	}
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// verifyFileDigest hashes the file and compares hex forms; label names the
// file in the failure text (the pinned asset/binary name, not the temp or
// install path). A mismatch is the integrity failure the spec names: the
// error text names the file class and the expectation, never the observed
// digest (no attacker-controlled state echoed back).
func verifyFileDigest(path, label, want string) error {
	if want == "" {
		return fmt.Errorf("%s has no pinned digest; refusing to run (incomplete pin)", label)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", label, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash %s: %w", label, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != strings.ToLower(want) {
		return fmt.Errorf("%s failed its SHA-256 pin; refusing to run — re-enable to re-download the pinned release", label)
	}
	return nil
}

// extractArchive unpacks a verified archive into dir, regular files only,
// every member name jail-checked (no absolute paths, no "..") so a pinned
// archive can still never write outside the install dir.
func extractArchive(archivePath, format, dir string) error {
	switch format {
	case "tar.gz":
		return extractTarGz(archivePath, dir)
	case "zip":
		return extractZip(archivePath, dir)
	default:
		return fmt.Errorf("unsupported archive format %q", format)
	}
}

// safeJoin resolves one archive member name inside dir, rejecting escapes.
func safeJoin(dir, name string) (string, error) {
	cleaned := filepath.Clean(name)
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive member %q escapes the install dir", name)
	}
	return filepath.Join(dir, cleaned), nil
}

// extractTarGz unpacks a tar.gz, creating parent directories as needed.
func extractTarGz(archivePath, dir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			continue // created on demand below
		case tar.TypeReg:
		default:
			continue // symlinks and specials are never part of the driver
		}
		dest, err := safeJoin(dir, hdr.Name)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
}

// extractZip unpacks a zip the same way.
func extractZip(archivePath, dir string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		dest, err := safeJoin(dir, zf.Name)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		src, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			_ = src.Close()
			return err
		}
		_, err = io.Copy(out, src)
		_ = src.Close()
		if err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
	return nil
}
