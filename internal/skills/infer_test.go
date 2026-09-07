package skills

import (
	"slices"
	"strings"
	"testing"
)

func TestDependenciesFromFrontmatter(t *testing.T) {
	content := `---
name: pdf-tools
description: Work with PDFs
dependencies:
  tools:
    - web.search
    - execute
  binaries: [pdftotext, qpdf]
  python:
    - pypdf>=4.0
---
# PDF tools
`
	tools, binaries, python := DependenciesFromFrontmatter(content)
	if !slices.Equal(tools, []string{"web.search", "execute"}) {
		t.Errorf("tools = %v", tools)
	}
	if !slices.Equal(binaries, []string{"pdftotext", "qpdf"}) {
		t.Errorf("binaries = %v", binaries)
	}
	if !slices.Equal(python, []string{"pypdf>=4.0"}) {
		t.Errorf("python = %v", python)
	}
}

func TestDependenciesFromFrontmatterMalformed(t *testing.T) {
	tools, binaries, python := DependenciesFromFrontmatter("no frontmatter at all")
	if len(tools)+len(binaries)+len(python) != 0 {
		t.Errorf("malformed frontmatter must yield nothing, got %v %v %v", tools, binaries, python)
	}
	if _, _, _ = DependenciesFromFrontmatter("---\nunclosed: [\n---\nbody"); len(tools) != 0 {
		t.Errorf("unclosed frontmatter must yield nothing")
	}
}

func TestFrontmatterField(t *testing.T) {
	content := "---\nname: my-skill\ndescription: does things\nversion: 1.2.3\n---\nbody"
	if got := FrontmatterField(content, "name"); got != "my-skill" {
		t.Errorf("name = %q", got)
	}
	if got := FrontmatterField(content, "description"); got != "does things" {
		t.Errorf("description = %q", got)
	}
	if got := FrontmatterField(content, "missing"); got != "" {
		t.Errorf("missing = %q", got)
	}
}

func TestInferDependencies(t *testing.T) {
	staged := &StagedSkill{
		Name: "pdf-tools",
		Files: []StagedFile{
			{Path: "SKILL.md", Content: []byte("---\ndescription: pdf work\n---\nUse web.search to find docs, then grep them.")},
			{Path: "scripts/requirements.txt", Content: []byte("# pinned\npypdf>=4.0\nreportlab\n--index-url https://evil.example\nrequests==2.31.0")},
			{Path: "scripts/run.py", Content: []byte("import pypdf\nimport os\nfrom bs4 import BeautifulSoup\nimport json\n")},
			{Path: "references/api.md", Content: []byte("docs")},
		},
	}
	before := make([]string, len(staged.Files))
	for i, f := range staged.Files {
		before[i] = string(f.Content)
	}

	set := InferDependencies(staged)

	// Imported files must be byte-faithful.
	for i, f := range staged.Files {
		if string(f.Content) != before[i] {
			t.Errorf("file %q was modified by inference", f.Path)
		}
	}

	// requirements.txt feeds the declaration; option lines are dropped;
	// imports add undeclared modules; stdlib imports are skipped.
	for _, want := range []string{"pypdf>=4.0", "reportlab", "requests==2.31.0", "bs4"} {
		found := false
		for _, dep := range set.Python {
			if dep.Requirement == want {
				found = true
			}
		}
		if !found {
			t.Errorf("python deps missing %q: %+v", want, set.Python)
		}
	}
	for _, dep := range set.Python {
		if strings.Contains(dep.Requirement, "index-url") {
			t.Errorf("custom index option must never be stored: %q", dep.Requirement)
		}
	}

	// scripts/ non-empty implies execute; body mentions web.search and grep.
	toolNames := map[string]bool{}
	for _, dep := range set.Tools {
		toolNames[dep.Name] = true
	}
	for _, want := range []string{"execute", "web.search", "grep"} {
		if !toolNames[want] {
			t.Errorf("tool deps missing %q: %+v", want, set.Tools)
		}
	}
}

func TestParseRequirements(t *testing.T) {
	reqs := ParseRequirements("pypdf>=4.0\n\n  # comment\n--index-url https://x\nfoo==1 @ file:///tmp\nbar\n")
	if !slices.Equal(reqs, []string{"pypdf>=4.0", "bar"}) {
		t.Errorf("reqs = %v", reqs)
	}
}

func TestRequirementNameAndConflictDetection(t *testing.T) {
	if got := RequirementName("Py_PDF>=4.0"); got != "py-pdf" {
		t.Errorf("RequirementName = %q", got)
	}
	err := checkConflicts([]SkillRequirements{
		{Skill: "a", Requirements: []string{"pypdf>=4.0"}},
		{Skill: "b", Requirements: []string{"pypdf<5"}},
	})
	if !IsDependencyConflict(err) {
		t.Fatalf("expected conflict error, got %v", err)
	}
	if err := checkConflicts([]SkillRequirements{
		{Skill: "a", Requirements: []string{"pypdf>=4.0"}},
		{Skill: "b", Requirements: []string{"pypdf>=4.0"}},
	}); err != nil {
		t.Errorf("identical requirements are not a conflict: %v", err)
	}
}
