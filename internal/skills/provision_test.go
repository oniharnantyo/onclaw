package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPythonProvisionerConflictNamesBothSides(t *testing.T) {
	p := NewPythonProvisioner(&fakeRunner{})
	skillsRoot := t.TempDir()
	_, err := p.Provision(context.Background(), ProvisionRequest{
		SkillsRoot: skillsRoot,
		Requirements: []SkillRequirements{
			{Skill: "pdf-tools", Requirements: []string{"pypdf>=4.0"}},
			{Skill: "archiver", Requirements: []string{"pypdf<5", "requests"}},
		},
	})
	if !IsDependencyConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"pdf-tools", "archiver", "pypdf"} {
		if !strings.Contains(msg, want) {
			t.Errorf("conflict error %q must name %q", msg, want)
		}
	}
}

func TestPythonProvisionerWithoutPythonReportsUnprovisioned(t *testing.T) {
	// lookPath empty: python3 absent, venv interpreter absent.
	p := NewPythonProvisioner(&fakeRunner{})
	statuses, err := p.Check(context.Background(), CheckRequest{
		SkillsRoot: t.TempDir(),
		Requirements: []SkillRequirements{
			{Skill: "pdf-tools", Requirements: []string{"pypdf>=4.0"}},
		},
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(statuses) != 1 || statuses[0].Status != DepUnprovisioned {
		t.Fatalf("statuses = %+v", statuses)
	}
}

func TestPythonProvisionerProvisionsUnionAndVerifies(t *testing.T) {
	runner := &fakeRunner{lookPath: map[string]string{
		PythonBinary: "/usr/bin/python3",
	}}
	skillsRoot := t.TempDir()
	// Pre-create the venv interpreter so the venv step is skipped.
	bin := filepath.Join(skillsRoot, ".venv", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	interp := filepath.Join(bin, "python")
	if err := os.WriteFile(interp, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner.lookPath[interp] = interp

	p := NewPythonProvisioner(runner)
	statuses, err := p.Provision(context.Background(), ProvisionRequest{
		SkillsRoot: skillsRoot,
		Requirements: []SkillRequirements{
			{Skill: "a", Requirements: []string{"pypdf>=4.0"}},
			{Skill: "b", Requirements: []string{"pypdf>=4.0", "reportlab"}},
		},
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("statuses = %+v", statuses)
	}
	for _, s := range statuses {
		if s.Status != DepMet {
			t.Errorf("status for %q = %q, want met", s.Requirement, s.Status)
		}
	}

	// Exactly one pip invocation, union-installing the deduplicated set,
	// with no index-url and a bounded timeout flag.
	var pip *fakeRun
	imports := 0
	for i := range runner.runs {
		r := &runner.runs[i]
		if strings.Contains(strings.Join(r.args, " "), "pip install") {
			pip = r
		}
		if strings.HasPrefix(strings.Join(r.args, " "), "-c import") || strings.Contains(strings.Join(r.args, " "), "-c import") {
			imports++
		}
	}
	if pip == nil {
		t.Fatalf("no pip install recorded: %+v", runner.runs)
	}
	joined := strings.Join(pip.args, " ")
	for _, want := range []string{"--no-input", "pypdf>=4.0", "reportlab"} {
		if !strings.Contains(joined, want) {
			t.Errorf("pip args missing %q: %v", want, pip.args)
		}
	}
	if strings.Count(joined, "pypdf>=4.0") != 1 {
		t.Errorf("union install must deduplicate: %v", pip.args)
	}
	if strings.Contains(joined, "index-url") {
		t.Errorf("custom index must never appear: %v", pip.args)
	}
	if imports != 2 {
		t.Errorf("import verification runs = %d, want 2 (one per package): %+v", imports, runner.runs)
	}
}

func TestVenvDirLayout(t *testing.T) {
	if got := VenvDir(filepath.Join("w", "acme", "skills")); got != filepath.Join("w", "acme", "skills", ".venv") {
		t.Errorf("VenvDir = %q", got)
	}
}

func TestCheckBinaries(t *testing.T) {
	runner := &fakeRunner{lookPath: map[string]string{"sh": "/bin/sh"}}
	deps := CheckBinaries(runner, []string{"pdftotext", "sh"})
	if len(deps) != 2 {
		t.Fatalf("deps = %+v", deps)
	}
	// Sorted by name: pdftotext, sh.
	if deps[0].Status != DepMissing || deps[0].InstallHint == "" {
		t.Errorf("missing binary not reported with hint: %+v", deps[0])
	}
	if deps[1].Status != DepMet {
		t.Errorf("present binary not met: %+v", deps[1])
	}
}

func TestProvisionerRegistry(t *testing.T) {
	p := NewPythonProvisioner(&fakeRunner{})
	RegisterProvisioner(p)
	got, ok := ProvisionerFor("python")
	if !ok || got.Kind() != "python" {
		t.Fatalf("registry lookup failed: %v %v", got, ok)
	}
	if _, ok := ProvisionerFor("nope"); ok {
		t.Errorf("unknown kind must not resolve")
	}
}
