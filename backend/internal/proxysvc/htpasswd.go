package proxysvc

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// The site form has a "password file" field, and nothing to put in it.
//
// That is the gap that makes basic auth useless from the dashboard: pointing
// at /etc/nginx/.htpasswd only helps somebody who has already been to a
// terminal and run htpasswd, which is the thing this page exists to avoid.
// Putting a staging site behind a password is one of the two or three most
// common reasons to reach for a reverse proxy at all.
//
// bcrypt rather than shelling to htpasswd: the tool is in apache2-utils, which
// is not installed on a host running nginx, and nginx has understood bcrypt
// hashes for over a decade. Doing it in-process also means the password never
// becomes an argv, which /proc/*/cmdline would make world-readable.

// htpasswdDir is where these files live — beside the proxy config they belong
// to, and inside the directory the config editor already guards.
const htpasswdDir = "/etc/nginx/jd-auth"

var (
	authFileRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	// A username may not contain a colon: the file format is user:hash, and a
	// colon in the first field silently truncates it.
	authUserRe = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,64}$`)
)

// AuthFile is one password file and who is in it.
type AuthFile struct {
	Name  string   `json:"name"`
	Path  string   `json:"path"`
	Users []string `json:"users"`
}

// ListAuthFiles enumerates the password files this dashboard manages.
func ListAuthFiles() []AuthFile {
	out := []AuthFile{}
	entries, err := os.ReadDir(htpasswdDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !authFileRe.MatchString(e.Name()) {
			continue
		}
		users, err := readAuthUsers(filepath.Join(htpasswdDir, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, AuthFile{
			Name: e.Name(), Path: filepath.Join(htpasswdDir, e.Name()), Users: users,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func readAuthUsers(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	users := []string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if user, _, ok := strings.Cut(line, ":"); ok && user != "" {
			users = append(users, user)
		}
	}
	sort.Strings(users)
	return users, sc.Err()
}

// SetAuthUser adds or replaces one entry, creating the file if needed.
func SetAuthUser(file, user, password string) (*AuthFile, error) {
	if !authFileRe.MatchString(file) {
		return nil, fmt.Errorf("file name must be lowercase letters, digits, dots, dashes or underscores")
	}
	if !authUserRe.MatchString(user) {
		return nil, fmt.Errorf("%q is not a valid user name", user)
	}
	// bcrypt silently truncates at 72 bytes, so a longer password would
	// quietly become a shorter one — worth refusing rather than accepting a
	// password that is not the one somebody typed.
	if len(password) < 8 {
		return nil, fmt.Errorf("the password must be at least 8 characters")
	}
	if len(password) > 72 {
		return nil, fmt.Errorf("the password must be at most 72 characters, which is bcrypt's limit")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(htpasswdDir, file)
	lines, err := readAuthLines(path)
	if err != nil {
		return nil, err
	}
	entry := user + ":" + string(hash)
	replaced := false
	for i, line := range lines {
		if existing, _, ok := strings.Cut(line, ":"); ok && existing == user {
			lines[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, entry)
	}
	if err := writeAuthFile(path, lines); err != nil {
		return nil, err
	}
	users, _ := readAuthUsers(path)
	return &AuthFile{Name: file, Path: path, Users: users}, nil
}

// RemoveAuthUser deletes one entry. Removing the last one leaves an empty
// file rather than deleting it: a site whose auth_basic_user_file has vanished
// stops nginx from starting, and an empty file simply admits nobody.
func RemoveAuthUser(file, user string) (*AuthFile, error) {
	if !authFileRe.MatchString(file) {
		return nil, fmt.Errorf("invalid file name")
	}
	path := filepath.Join(htpasswdDir, file)
	lines, err := readAuthLines(path)
	if err != nil {
		return nil, err
	}
	kept := lines[:0]
	found := false
	for _, line := range lines {
		if existing, _, ok := strings.Cut(line, ":"); ok && existing == user {
			found = true
			continue
		}
		kept = append(kept, line)
	}
	if !found {
		return nil, fmt.Errorf("%q is not in %s", user, file)
	}
	if err := writeAuthFile(path, kept); err != nil {
		return nil, err
	}
	users, _ := readAuthUsers(path)
	return &AuthFile{Name: file, Path: path, Users: users}, nil
}

// DeleteAuthFile removes a password file entirely.
func DeleteAuthFile(file string) error {
	if !authFileRe.MatchString(file) {
		return fmt.Errorf("invalid file name")
	}
	return os.Remove(filepath.Join(htpasswdDir, file))
}

func readAuthLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	lines := []string{}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines, nil
}

// writeAuthFile writes through a temporary file in the same directory. Mode
// 0640: nginx reads it as its own user, and nothing else has any business
// reading a file full of password hashes.
func writeAuthFile(path string, lines []string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".jd-auth-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	content := ""
	if len(lines) > 0 {
		content = strings.Join(lines, "\n") + "\n"
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o640); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
