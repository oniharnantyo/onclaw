package skills

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildZip builds an in-memory zip from (name, mode, content) entries.
func buildZip(t *testing.T, entries []struct {
	Name    string
	Mode    os.FileMode
	Content []byte
}) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		fh := &zip.FileHeader{Name: e.Name, Method: zip.Deflate}
		fh.SetMode(e.Mode)
		f, err := w.CreateHeader(fh)
		if err != nil {
			t.Fatalf("create entry %q: %v", e.Name, err)
		}
		if _, err := f.Write(e.Content); err != nil {
			t.Fatalf("write entry %q: %v", e.Name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestStageZipHostileArchives(t *testing.T) {
	skillMD := []byte("---\nname: test-skill\ndescription: d\n---\nbody\n")
	big := bytes.Repeat([]byte{0}, MaxStagedFileBytes+1)
	manyEntries := make([]struct {
		Name    string
		Mode    os.FileMode
		Content []byte
	}, 0, MaxArchiveFileCount+1)
	manyEntries = append(manyEntries, struct {
		Name    string
		Mode    os.FileMode
		Content []byte
	}{"SKILL.md", 0o644, skillMD})
	for i := 0; i < MaxArchiveFileCount; i++ {
		manyEntries = append(manyEntries, struct {
			Name    string
			Mode    os.FileMode
			Content []byte
		}{Name: strings.Repeat("f", 1) + "/" + "n" + itoa(i) + ".txt", Mode: 0o644, Content: []byte("x")})
	}

	tests := []struct {
		name    string
		archive []byte
		reason  string
	}{
		{
			name: "zip-slip parent traversal",
			archive: buildZip(t, []struct {
				Name    string
				Mode    os.FileMode
				Content []byte
			}{{Name: "SKILL.md", Mode: 0o644, Content: skillMD}, {Name: "../evil.txt", Mode: 0o644, Content: []byte("pwn")}}),
			reason:  "escapes",
		},
		{
			name: "zip-slip nested traversal",
			archive: buildZip(t, []struct {
				Name    string
				Mode    os.FileMode
				Content []byte
			}{{Name: "SKILL.md", Mode: 0o644, Content: skillMD}, {Name: "sub/../../evil.txt", Mode: 0o644, Content: []byte("pwn")}}),
			reason:  "not a clean relative path",
		},
		{
			name: "absolute path entry",
			archive: buildZip(t, []struct {
				Name    string
				Mode    os.FileMode
				Content []byte
			}{{Name: "/etc/passwd", Mode: 0o644, Content: []byte("root")}}),
			reason:  "absolute",
		},
		{
			name: "symlink entry",
			archive: buildZip(t, []struct {
				Name    string
				Mode    os.FileMode
				Content []byte
			}{{Name: "SKILL.md", Mode: 0o644, Content: skillMD}, {Name: "link", Mode: os.ModeSymlink | 0o777, Content: []byte("/etc")}}),
			reason:  "symlink",
		},
		{
			name: "missing SKILL.md at root",
			archive: buildZip(t, []struct {
				Name    string
				Mode    os.FileMode
				Content []byte
			}{{Name: "README.md", Mode: 0o644, Content: []byte("no skill")}, {Name: "nested/SKILL.md", Mode: 0o644, Content: skillMD}}),
			reason:  "SKILL.md is missing from the archive root",
		},
		{
			name: "too many files",
			archive: buildZip(t, manyEntries),
			reason:  "cap is",
		},
		{
			name: "single file too big",
			archive: buildZip(t, []struct {
				Name    string
				Mode    os.FileMode
				Content []byte
			}{{Name: "SKILL.md", Mode: 0o644, Content: skillMD}, {Name: "big.bin", Mode: 0o644, Content: big}}),
			reason:  "per-file cap",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			staged, err := StageZip(tt.archive)
			if err == nil {
				t.Fatalf("expected rejection, got staged skill %+v", staged)
			}
			if !IsHostileArchive(err) {
				t.Fatalf("expected hostile-archive error, got %v", err)
			}
			if !strings.Contains(err.Error(), tt.reason) {
				t.Errorf("error %q does not mention %q", err, tt.reason)
			}
		})
	}
}

func TestStageZipTotalSizeCap(t *testing.T) {
	skillMD := []byte("---\nname: s\ndescription: d\n---\nb\n")
	chunk := bytes.Repeat([]byte{0}, 4<<20) // 4 MiB each; 6 files > 20 MiB total
	entries := []struct {
		Name    string
		Mode    os.FileMode
		Content []byte
	}{{Name: "SKILL.md", Mode: 0o644, Content: skillMD}}
	for i := 0; i < 6; i++ {
		entries = append(entries, struct {
			Name    string
			Mode    os.FileMode
			Content []byte
		}{Name: "data" + itoa(i) + ".bin", Mode: 0o644, Content: chunk})
	}
	_, err := StageZip(buildZip(t, entries))
	if !IsHostileArchive(err) || !strings.Contains(err.Error(), "total size cap") {
		t.Fatalf("expected total-size rejection, got %v", err)
	}
}

func TestStageZipHappyPathPreservesBundles(t *testing.T) {
	skillMD := "---\nname: bundle\ndescription: bundled\nversion: 2.0.0\n---\nbody"
	archive := buildZip(t, []struct {
		Name    string
		Mode    os.FileMode
		Content []byte
	}{
		{Name: "SKILL.md", Mode: 0o644, Content: []byte(skillMD)},
		{Name: "scripts/run.sh", Mode: 0o755, Content: []byte("#!/bin/sh\necho hi")},
		{Name: "references/api.md", Mode: 0o644, Content: []byte("docs")},
	})
	staged, err := StageZip(archive)
	if err != nil {
		t.Fatalf("StageZip: %v", err)
	}
	if staged.Name != "bundle" || staged.Description != "bundled" || staged.Version != "2.0.0" {
		t.Errorf("metadata = %+v", staged)
	}
	if len(staged.Files) != 3 {
		t.Fatalf("files = %+v", staged.Files)
	}
	if stagedFile(staged, "scripts/run.sh") == nil || stagedFile(staged, "references/api.md") == nil {
		t.Errorf("bundled scripts/references not preserved: %+v", staged.Files)
	}
}

func TestStageDirectoryRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: d\n---\nb"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "SKILL.md"), filepath.Join(dir, "link")); err != nil {
		t.Skip("symlinks unavailable on this platform")
	}
	if _, err := StageDirectory(dir); !IsHostileArchive(err) {
		t.Fatalf("expected hostile-archive error for symlink tree, got %v", err)
	}
}

func TestStageDirectoryRequiresSkillMD(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := StageDirectory(dir); !IsHostileArchive(err) {
		t.Fatalf("expected rejection, got %v", err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for ; i > 0; i /= 10 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
	}
	return string(digits)
}
