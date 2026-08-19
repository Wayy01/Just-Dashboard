// Package safepath holds the containment rules that archive extraction and
// backup restore both need.
//
// Both features unpack a tar stream whose contents an attacker may control, as
// root, onto a host whose security boundary is the network perimeter rather
// than the process. They used to carry a copy each of the same lexical
// prefix test, and the same hole in it: filepath.Join drops the leading
// separator of an absolute second element, so an entry declaring the symlink
// target "/etc/cron.d" collapsed to "etc/cron.d", landed inside the
// destination, and passed. The next entry named "<link>/evil" then passed the
// same lexical test and was opened through the link — an arbitrary root write
// from a role holding nothing but file.write.
//
// The rules here are therefore stricter than "the cleaned path starts with the
// destination":
//
//   - a symlink target that is absolute is refused outright;
//   - a relative target is refused unless it resolves inside the destination;
//   - no entry is written through a symlink that already exists in the
//     destination, so an archive cannot plant a link and then follow it;
//   - the final component is unlinked rather than opened when it is a symlink,
//     which keeps re-extracting the same archive working.
package safepath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Join resolves an archive entry name against the destination directory.
//
// It refuses anything that would land outside dest, and anything whose parent
// directories cross a symlink that is already there. The final component is
// deliberately allowed to be an existing symlink: the callers replace it, and
// refusing it would break extracting the same archive twice.
func Join(dest, name string) (string, error) {
	dest = filepath.Clean(dest)
	cleaned := filepath.Clean(filepath.Join(dest, name))
	if cleaned != dest && !strings.HasPrefix(cleaned, dest+string(os.PathSeparator)) {
		return "", fmt.Errorf("entry %q would escape the destination directory", name)
	}
	rel, err := filepath.Rel(dest, cleaned)
	if err != nil {
		return "", fmt.Errorf("entry %q would escape the destination directory", name)
	}
	if rel == "." {
		return cleaned, nil
	}
	// Walk down from the destination and stop at the first symlink. Checking
	// the whole chain rather than only the parent is what closes the
	// plant-a-link-then-write-through-it sequence, since the link and the file
	// that follows it are separate entries in the same archive.
	current := dest
	parts := strings.Split(rel, string(os.PathSeparator))
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				// Nothing exists from here down, so nothing can be crossed.
				break
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("entry %q would be written through the symlink %q", name, current)
		}
	}
	return cleaned, nil
}

// CheckLinkTarget validates a symlink entry before it is created. name is the
// entry's own name inside the archive, linkname the target it declares.
func CheckLinkTarget(dest, name, linkname string) error {
	if linkname == "" {
		return fmt.Errorf("entry %q has an empty symlink target", name)
	}
	if filepath.IsAbs(linkname) {
		// The whole class the old lexical test silently accepted.
		return fmt.Errorf("entry %q points at the absolute path %q", name, linkname)
	}
	if _, err := Join(dest, filepath.Join(filepath.Dir(name), linkname)); err != nil {
		return fmt.Errorf("entry %q points outside the destination directory: %w", name, err)
	}
	return nil
}

// Mkdir creates a directory for an archive entry, replacing a symlink sitting
// in its place rather than following it.
func Mkdir(path string, perm os.FileMode) error {
	if err := replaceSymlink(path); err != nil {
		return err
	}
	return os.MkdirAll(path, perm)
}

// MkdirParents creates the parents of an entry. Join has already established
// that nothing between dest and path is a symlink.
func MkdirParents(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

// Create opens a regular archive entry for writing. O_NOFOLLOW is the last line
// of defence: if anything raced a symlink into place after Join looked, the
// open fails rather than writing through it.
func Create(path string, perm os.FileMode) (*os.File, error) {
	if err := replaceSymlink(path); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, perm)
}

// Symlink creates an entry's symlink, replacing whatever is in its place.
func Symlink(linkname, path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(linkname, path)
}

func replaceSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	return os.Remove(path)
}
