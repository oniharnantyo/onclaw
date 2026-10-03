package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oniharnantyo/onclaw-bench/suite"
)

// LoadSuites loads the run's task suites from spec, which is either a suite
// file path or a directory of suite files.
//
// A directory loads every *.json file EXCEPT files whose base name starts
// with "_" — the underscore prefix marks helper/draft suites that are not
// part of the default load (documented in the README). An explicit file path
// always loads, underscore prefix included (that is how the stub suite is
// run: --suite suites/_stub.json).
//
// Every loaded suite is linted (suite.Lint) before anything runs; any issue
// aborts with the full issue list printed. All loaded suites must share one
// non-empty suite_version — a mismatch (or an empty version) is an error, and
// task ids must be unique across the whole load because scoreboard rows and
// session ids are keyed by task id.
func LoadSuites(spec string) ([]suite.TaskSuite, error) {
	paths, err := suitePaths(spec)
	if err != nil {
		return nil, err
	}

	suites := make([]suite.TaskSuite, 0, len(paths))
	var lintIssues []string
	version := ""
	for _, path := range paths {
		s, err := suite.Load(path)
		if err != nil {
			return nil, err
		}
		for _, issue := range suite.Lint(s) {
			lintIssues = append(lintIssues, formatIssue(path, issue))
		}
		switch {
		case version == "":
			version = s.SuiteVersion
		case s.SuiteVersion != version:
			return nil, fmt.Errorf("suite version mismatch: %s declares %q, earlier files declare %q — every lane file in one run must carry the same non-empty suite_version", path, s.SuiteVersion, version)
		}
		suites = append(suites, s)
	}

	if version == "" {
		return nil, fmt.Errorf("loaded suites carry an empty suite_version")
	}
	if len(lintIssues) > 0 {
		return nil, fmt.Errorf("suite lint failed (%d issue%s):\n%s", len(lintIssues), plural(len(lintIssues)), strings.Join(lintIssues, "\n"))
	}

	seen := map[string]string{}
	for _, s := range suites {
		for i := range s.Tasks {
			id := s.Tasks[i].ID
			if prev, dup := seen[id]; dup {
				return nil, fmt.Errorf("duplicate task id %q across suites (%s and %s) — task ids must be unique in one run", id, prev, s.Lane)
			}
			seen[id] = s.Lane
		}
	}
	return suites, nil
}

// suitePaths resolves spec into the sorted list of suite files to load.
func suitePaths(spec string) ([]string, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, fmt.Errorf("suite path is empty: pass --suite <file.json|dir>")
	}
	info, err := os.Stat(spec)
	if err != nil {
		return nil, fmt.Errorf("suite path %s: %w", spec, err)
	}
	if !info.IsDir() {
		return []string{spec}, nil
	}

	entries, err := os.ReadDir(spec)
	if err != nil {
		return nil, fmt.Errorf("reading suite directory %s: %w", spec, err)
	}
	var paths []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		// Underscore-prefixed files are helper suites, excluded from
		// directory loads (they still load when named explicitly).
		if strings.HasPrefix(name, "_") {
			continue
		}
		paths = append(paths, filepath.Join(spec, name))
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("suite directory %s holds no loadable *.json suites (underscore-prefixed files are excluded from directory loads)", spec)
	}
	sort.Strings(paths)
	return paths, nil
}

func formatIssue(path string, issue suite.Issue) string {
	task := issue.TaskID
	if task == "" {
		task = "-"
	}
	return fmt.Sprintf("  %s [%s] %s: %s", path, task, issue.Field, issue.Message)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
