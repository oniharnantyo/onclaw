package skills

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Fetch tunables (deferred per design.md "Open Questions").
const (
	// MaxFetchBytes caps a downloaded archive.
	MaxFetchBytes = MaxArchiveBytes
	// FetchTimeout bounds the archive HTTP fetch.
	FetchTimeout = 5 * time.Minute
)

// ErrSSRF marks a refused git/URL target (private or internal ranges,
// non-HTTP(S) schemes such as file://) — a real risk on LAN self-hosts.
type ErrSSRF struct {
	Target string
	Reason string
}

func (e *ErrSSRF) Error() string {
	return fmt.Sprintf("refusing to fetch %q: %s", e.Target, e.Reason)
}

// IsSSRF reports whether err is an SSRF-guard refusal.
func IsSSRF(err error) bool {
	var ssrfErr *ErrSSRF
	return errors.As(err, &ssrfErr)
}

// GuardURL validates a git/archive target: http(s) only (file:// refused),
// and the resolved host must not sit in a private, loopback, link-local, or
// otherwise internal range.
func GuardURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return &ErrSSRF{Target: rawURL, Reason: fmt.Sprintf("scheme %q is not allowed (http/https only)", u.Scheme)}
	}
	host := u.Hostname()
	if host == "" {
		return &ErrSSRF{Target: rawURL, Reason: "URL has no host"}
	}
	if isDisallowedHost(host) {
		return &ErrSSRF{Target: rawURL, Reason: "host resolves to a private or internal address"}
	}
	return nil
}

// isDisallowedHost resolves host (a literal IP or a name) and reports
// whether any resolved address is internal.
func isDisallowedHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return isDisallowedIP(ip)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		// Unresolvable hosts are treated as disallowed: a fetch would fail
		// anyway and DNS-rebinding windows are exactly what the guard exists
		// to close.
		return true
	}
	for _, ip := range ips {
		if isDisallowedIP(ip) {
			return true
		}
	}
	return false
}

// isDisallowedIP reports whether an IP sits in a range a self-hosted server
// must never fetch from: loopback, private, link-local (unicast and
// multicast), unspecified, multicast, broadcast-style, unique-local, and
// carrier-grade NAT (100.64.0.0/10).
func isDisallowedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip.IsGlobalUnicast() {
		// Carrier-grade NAT is global-unicast but internal.
		if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1]&0xC0 == 64 {
			return true
		}
		return false
	}
	return true
}

// CommandRunner is the exec seam: production uses the real runner; tests
// script it.
type CommandRunner interface {
	// Run executes name with args in dir ("" = inherit), with extra env
	// prepended, returning combined output.
	Run(ctx context.Context, dir, name string, env []string, args ...string) ([]byte, error)
	// LookPath probes the server PATH for a binary.
	LookPath(name string) (string, error)
}

type execRunner struct{}

// NewExecRunner returns the production CommandRunner.
func NewExecRunner() CommandRunner { return execRunner{} }

func (execRunner) Run(ctx context.Context, dir, name string, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(env, os.Environ()...)
	}
	out, err := cmd.CombinedOutput()
	return out, err
}

func (execRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }

// Fetcher performs git clones and archive downloads behind the SSRF guard.
type Fetcher struct {
	runner CommandRunner
	client *http.Client
}

// NewFetcher builds a Fetcher from its granular dependencies.
func NewFetcher(runner CommandRunner, client *http.Client) *Fetcher {
	if client == nil {
		client = &http.Client{Timeout: FetchTimeout}
	}
	return &Fetcher{runner: runner, client: client}
}

// CloneGit performs a shallow, single-branch clone of url into dest — no
// submodule recursion, no tags. The one-time token (if any) is passed as an
// Authorization header on the fetch only and never persisted; ref may be a
// branch or tag name.
func (f *Fetcher) CloneGit(ctx context.Context, dest, rawURL, ref, token string) error {
	if err := GuardURL(rawURL); err != nil {
		return err
	}
	args := []string{"clone", "--depth", "1", "--single-branch", "--no-tags"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	if token != "" {
		args = append(args, []string{"-c", "http.extraHeader=Authorization: Bearer " + token}...)
	}
	args = append(args, rawURL, dest)
	out, err := f.runner.Run(ctx, "", "git", nil, args...)
	if err != nil {
		return fmt.Errorf("git clone failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// FetchArchive downloads an archive URL (behind the SSRF guard and the size
// cap) and returns its bytes. The token, when set, travels as a bearer
// header for this request only.
func (f *Fetcher) FetchArchive(ctx context.Context, rawURL, token string) ([]byte, error) {
	if err := GuardURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build archive request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch archive: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch archive: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxFetchBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	if int64(len(body)) > MaxFetchBytes {
		return nil, hostile(fmt.Sprintf("archive exceeds the fetch cap of %d bytes", MaxFetchBytes))
	}
	return body, nil
}

// DiscoveredSkill is one SKILL.md-bearing directory found in a fetched tree.
type DiscoveredSkill struct {
	// Name is the slugified directory name.
	Name string
	// RelDir is the directory path relative to the fetched tree root.
	RelDir string
	// Description comes from the discovered SKILL.md frontmatter.
	Description string
}

var slugSanitizePattern = regexp.MustCompile(`[^a-z0-9-]+`)

// SlugifyName coerces a directory or skill name into the DNS-label rules
// shared with agents and workspaces: lowercase, non-alphanumerics collapsed
// to single hyphens, trimmed of leading/trailing hyphens, capped at 63
// characters. The result must still pass domain.ValidateSlug (which also
// enforces the reserved list); an empty result means the name cannot be
// slugified.
func SlugifyName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugSanitizePattern.ReplaceAllString(s, "-")
	s = regexp.MustCompile(`-+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 63 {
		s = strings.Trim(s[:63], "-")
	}
	return s
}

// DiscoverSkills scans a fetched tree for directories containing SKILL.md
// (skipping .git), returning each with its slugified name and frontmatter
// description for wizard multi-select (spec: "Skill installation sources").
func DiscoverSkills(root string) ([]DiscoveredSkill, error) {
	var found []DiscoveredSkill
	err := walkTree(root, func(rel string, info fileInfo) error {
		if info.IsDir() || filepath.Base(rel) != "SKILL.md" {
			return nil
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		parts := strings.Split(dir, "/")
		if parts[0] == ".git" {
			return nil
		}
		var name string
		if dir == "." {
			// A SKILL.md at the tree root: the fetched tree itself is one
			// skill, named after the root directory.
			name = SlugifyName(filepath.Base(filepath.Clean(root)))
			dir = ""
		} else {
			name = SlugifyName(parts[len(parts)-1])
		}
		if name == "" {
			return nil // unsalvageable directory name; not importable
		}
		content, err := readFile(filepath.Join(root, rel))
		if err != nil {
			return err
		}
		found = append(found, DiscoveredSkill{
			Name:        name,
			RelDir:      dir,
			Description: FrontmatterField(string(content), "description"),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}
