// Package files backs the file manager. Every path that arrives from a client
// is resolved through Resolve before it is touched: that single choke point is
// what prevents ../ traversal and symlink escapes from reaching outside the
// configured roots.
package files

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	ErrOutsideRoot = errors.New("path is outside the permitted roots")
	ErrTooLarge    = errors.New("file is too large for the in-browser editor")
	ErrIsDir       = errors.New("path is a directory")
	ErrNotDir      = errors.New("path is not a directory")
)

// maxEditBytes bounds what the editor will load. Monaco is not usable beyond
// this and the browser tab would be, in practice, unusable well before it.
const maxEditBytes = 8 << 20

type Service struct {
	roots []string
}

func New(roots []string) *Service {
	cleaned := make([]string, 0, len(roots))
	for _, r := range roots {
		if abs, err := filepath.Abs(r); err == nil {
			cleaned = append(cleaned, filepath.Clean(abs))
		}
	}
	if len(cleaned) == 0 {
		cleaned = []string{"/"}
	}
	return &Service{roots: cleaned}
}

func (s *Service) Roots() []string { return s.roots }

// Resolve validates a client-supplied path. It checks the literal cleaned path
// and, when the target exists, the symlink-resolved one — a symlink inside a
// root pointing outside it must not become a way out. For a path that does not
// exist yet (a file being created) the parent directory is checked instead.
func (s *Service) Resolve(path string) (string, error) {
	if path == "" {
		path = s.roots[0]
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	if !s.within(abs) {
		return "", fmt.Errorf("%w: %s", ErrOutsideRoot, path)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
		if err != nil {
			// The parent does not exist either; the literal check above is
			// the strongest guarantee available and it already passed.
			return abs, nil
		}
		if !s.within(parent) {
			return "", fmt.Errorf("%w: %s", ErrOutsideRoot, path)
		}
		return filepath.Join(parent, filepath.Base(abs)), nil
	}
	if !s.within(resolved) {
		return "", fmt.Errorf("%w: %s resolves outside the permitted roots", ErrOutsideRoot, path)
	}
	return resolved, nil
}

func (s *Service) within(abs string) bool {
	for _, root := range s.roots {
		if root == "/" || abs == root || strings.HasPrefix(abs, root+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

type Entry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Size       int64     `json:"size"`
	Mode       string    `json:"mode"`
	ModeOctal  string    `json:"modeOctal"`
	IsDir      bool      `json:"isDir"`
	IsSymlink  bool      `json:"isSymlink"`
	LinkTarget string    `json:"linkTarget,omitempty"`
	LinkBroken bool      `json:"linkBroken,omitempty"`
	Modified   time.Time `json:"modified"`
	Owner      string    `json:"owner"`
	Group      string    `json:"group"`
	UID        uint32    `json:"uid"`
	GID        uint32    `json:"gid"`
	MimeHint   string    `json:"mimeHint,omitempty"`
	ChildCount *int      `json:"childCount,omitempty"`
}

type Listing struct {
	Path    string   `json:"path"`
	Parent  string   `json:"parent"`
	Entries []Entry  `json:"entries"`
	Roots   []string `json:"roots"`
}

func (s *Service) List(path string, showHidden bool) (*Listing, error) {
	dir, err := s.Resolve(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, ErrNotDir
	}
	names, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := &Listing{Path: dir, Parent: filepath.Dir(dir), Entries: []Entry{}, Roots: s.roots}
	for _, d := range names {
		if !showHidden && strings.HasPrefix(d.Name(), ".") {
			continue
		}
		full := filepath.Join(dir, d.Name())
		e, err := s.entry(full, d.Name())
		if err != nil {
			continue
		}
		out.Entries = append(out.Entries, *e)
	}
	// Directories first, then case-insensitive by name — the ordering every
	// file manager uses, and the one that makes navigation predictable.
	sort.Slice(out.Entries, func(i, j int) bool {
		if out.Entries[i].IsDir != out.Entries[j].IsDir {
			return out.Entries[i].IsDir
		}
		return strings.ToLower(out.Entries[i].Name) < strings.ToLower(out.Entries[j].Name)
	})
	return out, nil
}

func (s *Service) entry(full, name string) (*Entry, error) {
	// Lstat, not Stat: the listing must show the link itself, including one
	// whose target is missing.
	li, err := os.Lstat(full)
	if err != nil {
		return nil, err
	}
	e := &Entry{
		Name: name, Path: full, Size: li.Size(),
		Mode: li.Mode().String(), ModeOctal: fmt.Sprintf("%04o", li.Mode().Perm()),
		Modified: li.ModTime().UTC(),
	}
	if li.Mode()&os.ModeSymlink != 0 {
		e.IsSymlink = true
		if target, err := os.Readlink(full); err == nil {
			e.LinkTarget = target
		}
		target, err := os.Stat(full)
		if err != nil {
			e.LinkBroken = true
		} else {
			e.IsDir = target.IsDir()
			e.Size = target.Size()
		}
	} else {
		e.IsDir = li.IsDir()
	}
	if st, ok := li.Sys().(*syscall.Stat_t); ok {
		e.UID, e.GID = st.Uid, st.Gid
		e.Owner = lookupUser(st.Uid)
		e.Group = lookupGroup(st.Gid)
	}
	if !e.IsDir {
		e.MimeHint = mimeHint(name)
	}
	return e, nil
}

// User and group lookups hit NSS, which can be slow; a directory listing does
// hundreds of them, so results are memoised for the process lifetime.
var (
	userCache  = map[uint32]string{}
	groupCache = map[uint32]string{}
)

func lookupUser(uid uint32) string {
	if v, ok := userCache[uid]; ok {
		return v
	}
	name := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(name); err == nil {
		name = u.Username
	}
	userCache[uid] = name
	return name
}

func lookupGroup(gid uint32) string {
	if v, ok := groupCache[gid]; ok {
		return v
	}
	name := strconv.FormatUint(uint64(gid), 10)
	if g, err := user.LookupGroupId(name); err == nil {
		name = g.Name
	}
	groupCache[gid] = name
	return name
}

var editorLanguages = map[string]string{
	".go": "go", ".js": "javascript", ".mjs": "javascript", ".cjs": "javascript",
	".ts": "typescript", ".tsx": "typescript", ".jsx": "javascript",
	".json": "json", ".yml": "yaml", ".yaml": "yaml", ".toml": "toml",
	".md": "markdown", ".sh": "shell", ".bash": "shell", ".zsh": "shell",
	".py": "python", ".rb": "ruby", ".rs": "rust", ".php": "php",
	".sql": "sql", ".html": "html", ".css": "css", ".scss": "scss",
	".xml": "xml", ".ini": "ini", ".conf": "ini", ".env": "shell",
	".dockerfile": "dockerfile", ".service": "ini", ".nginx": "nginx",
}

func mimeHint(name string) string {
	if lang, ok := editorLanguages[strings.ToLower(filepath.Ext(name))]; ok {
		return lang
	}
	switch strings.ToLower(name) {
	case "dockerfile", "containerfile":
		return "dockerfile"
	case "makefile":
		return "makefile"
	case "caddyfile":
		return "caddyfile"
	}
	return "plaintext"
}

type FileContent struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
	Language string `json:"language"`
	Binary   bool   `json:"binary"`
	Mode     string `json:"modeOctal"`
}

func (s *Service) Read(path string) (*FileContent, error) {
	full, err := s.Resolve(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, ErrIsDir
	}
	if st.Size() > maxEditBytes {
		return nil, fmt.Errorf("%w: %d bytes exceeds the %d byte limit", ErrTooLarge, st.Size(), maxEditBytes)
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	fc := &FileContent{
		Path: full, Size: st.Size(),
		Language: mimeHint(filepath.Base(full)),
		Mode:     fmt.Sprintf("%04o", st.Mode().Perm()),
		Binary:   looksBinary(b),
	}
	if !fc.Binary {
		fc.Content = string(b)
	}
	return fc, nil
}

// looksBinary uses the same heuristic as most editors: a NUL byte in the first
// kilobyte means this is not text worth loading into a code editor.
func looksBinary(b []byte) bool {
	n := len(b)
	if n > 1024 {
		n = 1024
	}
	for i := 0; i < n; i++ {
		if b[i] == 0 {
			return true
		}
	}
	return false
}

// Write replaces a file's contents atomically: the new content lands in a
// temporary file in the same directory and is renamed over the target, so an
// interrupted save can never leave a half-written config behind.
func (s *Service) Write(path, content string) error {
	full, err := s.Resolve(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(full)
	perm := os.FileMode(0o644)
	var uid, gid = -1, -1
	if st, err := os.Stat(full); err == nil {
		if st.IsDir() {
			return ErrIsDir
		}
		perm = st.Mode().Perm()
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			uid, gid = int(sys.Uid), int(sys.Gid)
		}
	}
	tmp, err := os.CreateTemp(dir, ".vpsd-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if uid >= 0 {
		os.Chown(tmpName, uid, gid)
	}
	return os.Rename(tmpName, full)
}

func (s *Service) Mkdir(path string, mode os.FileMode) error {
	full, err := s.Resolve(path)
	if err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o755
	}
	return os.MkdirAll(full, mode)
}

func (s *Service) Touch(path string) error {
	full, err := s.Resolve(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(full); err == nil {
		return os.Chtimes(full, time.Now(), time.Now())
	}
	f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

func (s *Service) Delete(path string, recursive bool) error {
	full, err := s.Resolve(path)
	if err != nil {
		return err
	}
	for _, root := range s.roots {
		if full == root {
			return fmt.Errorf("refusing to delete the configured root %s", root)
		}
	}
	if recursive {
		return os.RemoveAll(full)
	}
	return os.Remove(full)
}

func (s *Service) Move(from, to string) error {
	src, err := s.Resolve(from)
	if err != nil {
		return err
	}
	dst, err := s.Resolve(to)
	if err != nil {
		return err
	}
	if st, err := os.Stat(dst); err == nil && st.IsDir() {
		dst = filepath.Join(dst, filepath.Base(src))
	}
	if err := os.Rename(src, dst); err != nil {
		// Rename fails across filesystems; fall back to copy-then-remove so
		// moving between / and a mounted volume works as the user expects.
		if linkErr, ok := err.(*os.LinkError); ok && linkErr.Err == syscall.EXDEV {
			if err := copyPath(src, dst); err != nil {
				return err
			}
			return os.RemoveAll(src)
		}
		return err
	}
	return nil
}

func (s *Service) Copy(from, to string) error {
	src, err := s.Resolve(from)
	if err != nil {
		return err
	}
	dst, err := s.Resolve(to)
	if err != nil {
		return err
	}
	if st, err := os.Stat(dst); err == nil && st.IsDir() {
		dst = filepath.Join(dst, filepath.Base(src))
	}
	return copyPath(src, dst)
}

func copyPath(src, dst string) error {
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case st.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case st.IsDir():
		if err := os.MkdirAll(dst, st.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyPath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	default:
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	}
}

func (s *Service) Chmod(path string, mode string, recursive bool) error {
	full, err := s.Resolve(path)
	if err != nil {
		return err
	}
	parsed, err := strconv.ParseUint(mode, 8, 32)
	if err != nil {
		return fmt.Errorf("mode must be octal, for example 0644: %w", err)
	}
	perm := os.FileMode(parsed).Perm()
	if !recursive {
		return os.Chmod(full, perm)
	}
	return filepath.WalkDir(full, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chmod(p, perm)
	})
}

func (s *Service) Chown(path, owner, group string, recursive bool) error {
	full, err := s.Resolve(path)
	if err != nil {
		return err
	}
	uid, gid := -1, -1
	if owner != "" {
		u, err := user.Lookup(owner)
		if err != nil {
			if n, convErr := strconv.Atoi(owner); convErr == nil {
				uid = n
			} else {
				return fmt.Errorf("unknown user %q", owner)
			}
		} else {
			uid, _ = strconv.Atoi(u.Uid)
		}
	}
	if group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			if n, convErr := strconv.Atoi(group); convErr == nil {
				gid = n
			} else {
				return fmt.Errorf("unknown group %q", group)
			}
		} else {
			gid, _ = strconv.Atoi(g.Gid)
		}
	}
	if !recursive {
		return os.Chown(full, uid, gid)
	}
	return filepath.WalkDir(full, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(p, uid, gid)
	})
}

func (s *Service) Symlink(target, link string) error {
	full, err := s.Resolve(link)
	if err != nil {
		return err
	}
	return os.Symlink(target, full)
}

// Stat is the single-entry form used by the details panel.
func (s *Service) Stat(path string) (*Entry, error) {
	full, err := s.Resolve(path)
	if err != nil {
		return nil, err
	}
	return s.entry(full, filepath.Base(full))
}

// Open returns a handle for download. The caller closes it.
func (s *Service) Open(path string) (*os.File, os.FileInfo, error) {
	full, err := s.Resolve(path)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, st, nil
}

// Create opens a destination for upload, refusing to clobber unless asked.
func (s *Service) Create(path string, overwrite bool) (*os.File, error) {
	full, err := s.Resolve(path)
	if err != nil {
		return nil, err
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if !overwrite {
		flags = os.O_CREATE | os.O_WRONLY | os.O_EXCL
	}
	return os.OpenFile(full, flags, 0o644)
}
