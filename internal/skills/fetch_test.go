package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardURL(t *testing.T) {
	tests := []struct {
		url   string
		ok    bool
		reason string
	}{
		{"file:///etc/passwd", false, "scheme"},
		{"ftp://example.com/x.zip", false, "scheme"},
		{"http://127.0.0.1/x.zip", false, "private or internal"},
		{"http://localhost/x.zip", false, "private or internal"},
		{"http://10.0.0.1/repo.git", false, "private or internal"},
		{"http://192.168.1.1/repo.git", false, "private or internal"},
		{"http://169.254.169.254/latest/meta-data", false, "private or internal"},
		{"http://[::1]/x", false, "private or internal"},
		{"http://100.64.0.1/x", false, "private or internal"},
		{"https://example.com/skill.zip", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			err := GuardURL(tt.url)
			if tt.ok && err != nil {
				t.Fatalf("expected allow, got %v", err)
			}
			if !tt.ok {
				if err == nil {
					t.Fatalf("expected refusal")
				}
				if !IsSSRF(err) {
					t.Fatalf("expected SSRF error, got %v", err)
				}
				if !strings.Contains(err.Error(), tt.reason) {
					t.Errorf("error %q missing reason %q", err, tt.reason)
				}
			}
		})
	}
}

// fakeRunner scripts CommandRunner.
type fakeRunner struct {
	lookPath map[string]string
	runs     []fakeRun
	failNext bool
}

type fakeRun struct {
	dir  string
	name string
	args []string
}

func (f *fakeRunner) Run(ctx context.Context, dir, name string, env []string, args ...string) ([]byte, error) {
	f.runs = append(f.runs, fakeRun{dir: dir, name: name, args: append([]string(nil), args...)})
	if name == "git" && strings.Contains(strings.Join(args, " "), "clone") && strings.Contains(strings.Join(args, " "), "fail-marker") {
		return []byte("boom"), errFake
	}
	if strings.Contains(name, "no-such") {
		return nil, errFake
	}
	return []byte("ok"), nil
}

func (f *fakeRunner) LookPath(name string) (string, error) {
	if p, ok := f.lookPath[name]; ok {
		return p, nil
	}
	return "", errFake
}

var errFake = &fakeError{}

type fakeError struct{}

func (*fakeError) Error() string { return "fake error" }

func TestCloneGitShallowSingleBranchNoSSRF(t *testing.T) {
	runner := &fakeRunner{}
	f := NewFetcher(runner, nil)

	if err := f.CloneGit(context.Background(), t.TempDir(), "http://192.168.1.5/repo.git", "", ""); err == nil {
		t.Fatal("private-range URL must be refused before any clone")
	}
	if len(runner.runs) != 0 {
		t.Fatalf("runner must not be invoked for refused URLs: %+v", runner.runs)
	}

	dest := t.TempDir()
	if err := f.CloneGit(context.Background(), dest, "https://example.com/repo.git", "v1.2", "sekrit"); err != nil {
		t.Fatalf("CloneGit: %v", err)
	}
	run := runner.runs[0]
	joined := strings.Join(run.args, " ")
	for _, want := range []string{"--depth 1", "--single-branch", "--branch v1.2", "http.extraHeader=Authorization: Bearer sekrit"} {
		if !strings.Contains(joined, want) {
			t.Errorf("clone args missing %q: %v", want, run.args)
		}
	}
	if strings.Contains(joined, "recurse-submodules") {
		t.Errorf("clone must not recurse submodules: %v", run.args)
	}
}

func TestSlugifyName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Web Research", "web-research"},
		{"My_Cool Skill!!", "my-cool-skill"},
		{"--leading--and--trailing--", "leading-and-trailing"},
		{"UPPER", "upper"},
		{"...", ""},
		{strings.Repeat("a", 100), strings.Trim(strings.Repeat("a", 63), "-")},
	}
	for _, tt := range tests {
		if got := SlugifyName(tt.in); got != tt.want {
			t.Errorf("SlugifyName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDiscoverSkills(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("alpha/SKILL.md", "---\nname: alpha\ndescription: first\n---\nb")
	write("beta/SKILL.md", "---\ndescription: second\n---\nb")
	write("beta/extra.md", "x")
	write(".git/config/SKILL.md", "fake") // .git internals must be skipped
	write("noskill/README.md", "x")

	found, err := DiscoverSkills(root)
	if err != nil {
		t.Fatalf("DiscoverSkills: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("found = %+v", found)
	}
	for _, d := range found {
		switch d.Name {
		case "alpha":
			if d.Description != "first" || d.RelDir != "alpha" {
				t.Errorf("alpha = %+v", d)
			}
		case "beta":
			if d.Description != "second" {
				t.Errorf("beta = %+v", d)
			}
		default:
			t.Errorf("unexpected skill %+v", d)
		}
	}
}

func TestDiscoverSkillsTreeRootSkill(t *testing.T) {
	root := t.TempDir()
	// The fetched tree itself is one skill when SKILL.md sits at its root.
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\ndescription: solo\n---\nb"), 0o644); err != nil {
		t.Fatal(err)
	}
	found, err := DiscoverSkills(root)
	if err != nil {
		t.Fatalf("DiscoverSkills: %v", err)
	}
	if len(found) != 1 || found[0].Description != "solo" {
		t.Fatalf("found = %+v", found)
	}
	if found[0].Name == "" {
		t.Errorf("root skill name derives from the tree root base name")
	}
}
