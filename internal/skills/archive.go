package skills

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// Archive caps are deferred tunables landed as constants (design.md
// "Open Questions"): adjust here without touching specs or structure.
const (
	// MaxArchiveBytes caps the total uncompressed size of an uploaded or
	// fetched archive.
	MaxArchiveBytes = 20 << 20 // 20 MiB
	// MaxArchiveFileCount caps how many entries an archive may carry.
	MaxArchiveFileCount = 500
	// MaxStagedFileBytes caps a single staged file.
	MaxStagedFileBytes = 5 << 20 // 5 MiB
)

// ErrHostileArchive marks a rejected archive (zip-slip, symlink entry,
// caps exceeded, missing SKILL.md). A rejected extraction writes nothing.
type ErrHostileArchive struct {
	Reason string
}

func (e *ErrHostileArchive) Error() string {
	return "archive rejected: " + e.Reason
}

func hostile(reason string) error { return &ErrHostileArchive{Reason: reason} }

// StageZip materializes an uploaded zip archive into staged skill files.
// Every entry must be a regular file with a relative, in-tree path —
// absolute paths, symlink entries (mode bit or `..`-escaping resolved
// targets), and entries resolving outside the skill directory are rejected
// (resolve-then-prefix-check). SKILL.md must exist at the archive root, and
// the total uncompressed size and entry count are capped.
func StageZip(archive []byte) (*StagedSkill, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("parse zip archive: %w", err)
	}
	if len(reader.File) > MaxArchiveFileCount {
		return nil, hostile(fmt.Sprintf("archive holds %d entries, cap is %d", len(reader.File), MaxArchiveFileCount))
	}

	root := "/" // conceptual skill root for prefix checks
	total := int64(0)
	hasSkillMD := false
	staged := &StagedSkill{Source: SourceUpload, Files: make([]StagedFile, 0, len(reader.File))}

	for _, entry := range reader.File {
		name := entry.Name
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) {
			return nil, hostile(fmt.Sprintf("entry %q is absolute or not a portable path", name))
		}
		if path.Clean(name) != name {
			return nil, hostile(fmt.Sprintf("entry %q is not a clean relative path", name))
		}
		if strings.HasPrefix(name, "../") || name == ".." {
			return nil, hostile(fmt.Sprintf("entry %q escapes the skill directory", name))
		}
		mode := entry.Mode()
		if mode&os.ModeSymlink != 0 {
			return nil, hostile(fmt.Sprintf("entry %q is a symlink", name))
		}
		if mode&os.ModeDir != 0 || entry.FileInfo().IsDir() {
			continue // directories are implied by file paths
		}
		if !mode.IsRegular() {
			return nil, hostile(fmt.Sprintf("entry %q is not a regular file", name))
		}
		// Resolve-then-prefix-check: the cleaned path joined to the conceptual
		// root must remain under it (catches residual traversal encodings;
		// symlink entries are rejected above so no link resolution applies).
		resolved := path.Join(root, name)
		if resolved == "/" || !strings.HasPrefix(resolved, "/") {
			return nil, hostile(fmt.Sprintf("entry %q resolves outside the skill directory", name))
		}

		if entry.UncompressedSize64 > uint64(MaxStagedFileBytes) {
			return nil, hostile(fmt.Sprintf("entry %q exceeds the per-file cap of %d bytes", name, MaxStagedFileBytes))
		}
		if total+int64(entry.UncompressedSize64) > MaxArchiveBytes {
			return nil, hostile("archive exceeds the total size cap")
		}
		total += int64(entry.UncompressedSize64)

		content, err := readZipEntry(entry)
		if err != nil {
			return nil, err
		}
		if int64(len(content)) > MaxStagedFileBytes {
			return nil, hostile(fmt.Sprintf("entry %q exceeds the per-file cap of %d bytes", name, MaxStagedFileBytes))
		}
		if name == "SKILL.md" {
			hasSkillMD = true
		}
		staged.Files = append(staged.Files, StagedFile{
			Path:    name,
			Content: content,
			Mode:    uint32(mode.Perm()),
		})
	}

	if !hasSkillMD {
		return nil, hostile("SKILL.md is missing from the archive root")
	}
	if total > MaxArchiveBytes {
		return nil, hostile("archive exceeds the total size cap")
	}

	skillMD := stagedFile(staged, "SKILL.md")
	staged.Name = SlugifyName(FrontmatterField(string(skillMD.Content), "name"))
	staged.Description = FrontmatterField(string(skillMD.Content), "description")
	staged.Version = FrontmatterField(string(skillMD.Content), "version")
	return staged, nil
}

// StageDirectory materializes a directory on disk (a walked upload folder
// or a discovered git-tree skill directory) into staged files, applying the
// same caps and symlink rejection as StageZip. SKILL.md must sit at the
// directory root.
func StageDirectory(dir string) (*StagedSkill, error) {
	staged := &StagedSkill{Source: SourceUpload}
	total := int64(0)
	count := 0

	err := walkTree(dir, func(rel string, info fileInfo) error {
		count++
		if count > MaxArchiveFileCount {
			return hostile(fmt.Sprintf("tree holds more than %d files", MaxArchiveFileCount))
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return hostile(fmt.Sprintf("entry %q is not a regular file", rel))
		}
		if info.Size() > MaxStagedFileBytes {
			return hostile(fmt.Sprintf("entry %q exceeds the per-file cap of %d bytes", rel, MaxStagedFileBytes))
		}
		total += info.Size()
		if total > MaxArchiveBytes {
			return hostile("tree exceeds the total size cap")
		}
		content, err := readFile(path.Join(dir, rel))
		if err != nil {
			return err
		}
		staged.Files = append(staged.Files, StagedFile{Path: rel, Content: content, Mode: uint32(info.Mode().Perm())})
		return nil
	})
	if err != nil {
		return nil, err
	}

	skillMD := stagedFile(staged, "SKILL.md")
	if skillMD == nil {
		return nil, hostile("SKILL.md is missing from the directory root")
	}
	staged.Name = SlugifyName(FrontmatterField(string(skillMD.Content), "name"))
	staged.Description = FrontmatterField(string(skillMD.Content), "description")
	staged.Version = FrontmatterField(string(skillMD.Content), "version")
	return staged, nil
}

// stagedFile returns the staged file at path, or nil.
func stagedFile(staged *StagedSkill, p string) *StagedFile {
	for i := range staged.Files {
		if staged.Files[i].Path == p {
			return &staged.Files[i]
		}
	}
	return nil
}

// readZipEntry decompresses one entry, enforcing the per-file cap against
// lying headers by capping the actual read too.
func readZipEntry(entry *zip.File) ([]byte, error) {
	rc, err := entry.Open()
	if err != nil {
		return nil, fmt.Errorf("open archive entry %q: %w", entry.Name, err)
	}
	defer rc.Close()
	content, err := io.ReadAll(io.LimitReader(rc, MaxStagedFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read archive entry %q: %w", entry.Name, err)
	}
	if int64(len(content)) > MaxStagedFileBytes {
		return nil, hostile(fmt.Sprintf("entry %q exceeds the per-file cap of %d bytes", entry.Name, MaxStagedFileBytes))
	}
	return content, nil
}

// IsHostileArchive reports whether err is an archive-rejection error.
func IsHostileArchive(err error) bool {
	var hostileErr *ErrHostileArchive
	return errors.As(err, &hostileErr)
}
