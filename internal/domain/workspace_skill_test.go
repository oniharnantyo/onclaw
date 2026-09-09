package domain

import (
	"strings"
	"testing"
)

func TestValidateSkillSource(t *testing.T) {
	for _, source := range []SkillSource{SkillSourceAuthored, SkillSourceUpload, SkillSourceGit, SkillSourceFork} {
		if err := ValidateSkillSource(source); err != nil {
			t.Errorf("source %q should be valid: %v", source, err)
		}
	}
	for _, source := range []SkillSource{"", "marketplace", "Author"} {
		if err := ValidateSkillSource(source); err == nil {
			t.Errorf("source %q should be invalid", source)
		}
	}
}

func TestValidateSkillVersion(t *testing.T) {
	for _, version := range []string{"0.1.0", "1.0.0", "12.34.56"} {
		if err := ValidateSkillVersion(version); err != nil {
			t.Errorf("version %q should be valid: %v", version, err)
		}
	}
	for _, version := range []string{"", "0.1", "1.0.0-beta", "v1.0.0", "01.0.0"} {
		if err := ValidateSkillVersion(version); err == nil {
			t.Errorf("version %q should be invalid", version)
		}
	}
}

func TestValidateSkillName(t *testing.T) {
	for _, name := range []string{"changelog-sweeper", "web-research", "a"} {
		if err := ValidateSkillName(name); err != nil {
			t.Errorf("name %q should be valid: %v", name, err)
		}
	}
	for _, name := range []string{"", "-leading", "trailing-", "Upper", "has space", "master", "settings", strings.Repeat("a", 64)} {
		if err := ValidateSkillName(name); err == nil {
			t.Errorf("name %q should be invalid", name)
		}
	}
}

func TestValidateSkillDependencies(t *testing.T) {
	valid := SkillDependencies{
		Tools:    []string{"web.search", "execute"},
		Binaries: []string{"pdftotext"},
		Python:   []string{"pypdf>=4.0"},
	}
	if err := ValidateSkillDependencies(valid); err != nil {
		t.Fatalf("valid dependencies rejected: %v", err)
	}

	for _, deps := range []SkillDependencies{
		{Tools: []string{"web search"}},
		{Tools: []string{" web.search"}},
		{Binaries: []string{""}},
		{Python: []string{"pkg/escape"}},
	} {
		if err := ValidateSkillDependencies(deps); err == nil {
			t.Errorf("dependencies %+v should be invalid", deps)
		}
	}
}

func TestWorkspaceSkillValidate(t *testing.T) {
	base := func() *WorkspaceSkill {
		return &WorkspaceSkill{
			WorkspaceID:  "ws-1",
			Name:         "changelog-sweeper",
			Description:  "Sweeps changelogs",
			Version:      DefaultSkillVersion,
			Source:       SkillSourceAuthored,
			Enabled:      true,
			Dependencies: SkillDependencies{Tools: []string{"execute"}},
		}
	}

	if err := base().Validate(); err != nil {
		t.Fatalf("valid skill rejected: %v", err)
	}

	cases := map[string]func(*WorkspaceSkill){
		"empty workspace id": func(s *WorkspaceSkill) { s.WorkspaceID = "" },
		"reserved name":      func(s *WorkspaceSkill) { s.Name = "api" },
		"invalid source":     func(s *WorkspaceSkill) { s.Source = "marketplace" },
		"invalid version":    func(s *WorkspaceSkill) { s.Version = "1.0" },
		"huge description":   func(s *WorkspaceSkill) { s.Description = strings.Repeat("x", MaxSkillDescriptionBytes+1) },
		"invalid dependency": func(s *WorkspaceSkill) { s.Dependencies.Binaries = []string{"has space"} },
	}
	for label, mutate := range cases {
		s := base()
		mutate(s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: expected validation failure", label)
		}
	}

	var nilSkill *WorkspaceSkill
	if err := nilSkill.Validate(); err == nil {
		t.Error("nil skill should fail validation")
	}
}
