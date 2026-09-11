package git

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxEntry bounds one file unpacked out of a git archive. A Skill is
// instructions, and anything larger than this is not one.
const maxEntry = 64 << 20

// maxEntries bounds how many files one archive may hold, so that a source
// nobody vetted cannot fill a disk with empty files.
const maxEntries = 10_000

// untar unpacks an archive as it arrives into a directory, refusing anything
// that would land outside it. The archive comes from `git archive`, which writes only the tree
// of one commit, but what is in that tree is somebody else's to decide: a
// Skill is a code-execution vector (ADR-0024), so what it may write is bounded
// here rather than trusted.
func untar(archive io.Reader, into string) error {
	root, err := filepath.EvalSymlinks(into)
	if err != nil {
		return err
	}
	r := tar.NewReader(archive)
	for n := 0; ; n++ {
		header, err := r.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading the archive: %w", err)
		}
		if n >= maxEntries {
			return fmt.Errorf("the archive holds more than %d files, which is not a skill", maxEntries)
		}
		path, err := safePath(root, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeXGlobalHeader:
			// git writes the commit it exported as a pax global header, which
			// is a record about the archive rather than a file in it.
			continue
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if header.Size > maxEntry {
				return fmt.Errorf("%s is %d bytes, which is larger than a skill's file may be",
					header.Name, header.Size)
			}
			if err := writeEntry(path, r, header.Size); err != nil {
				return err
			}
		default:
			// A symlink, a device, a hard link: a Skill is files, and anything
			// else is a way of reaching outside the directory it lands in.
			return fmt.Errorf("%s is not a file or a directory, which a skill may not hold", header.Name)
		}
	}
}

// safePath is where an archive entry lands. A name that would put it anywhere
// but under root is refused rather than cleaned into place: an archive that
// tries to escape is not a skill, and quietly relocating its files would hide
// that.
func safePath(root, name string) (string, error) {
	clean := filepath.Clean(name)
	if name == "" || filepath.IsAbs(name) || strings.HasPrefix(name, "/") ||
		clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q is not a path a skill may write to", name)
	}
	path := filepath.Join(root, clean)
	if path != root && !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return "", fmt.Errorf("%q would land outside %s", name, root)
	}
	return path, nil
}

// writeEntry writes one file, creating the directories above it. The mode is
// Owl's own rather than the archive's: what a Skill's author marked executable
// is not what Owl runs, and a file nobody but the owner can read is enough for
// something an Agent only reads.
func writeEntry(path string, r io.Reader, size int64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := io.CopyN(f, r, size); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return f.Close()
}
