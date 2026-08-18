package linuxusers

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

// SSHKey is one entry of an authorized_keys file, with the fingerprint
// computed the same way `ssh-keygen -lf` reports it so an operator can compare
// against what they have locally.
type SSHKey struct {
	Line        int    `json:"line"`
	Type        string `json:"type"`
	Comment     string `json:"comment"`
	Fingerprint string `json:"fingerprint"`
	Bits        int    `json:"bits,omitempty"`
	Options     string `json:"options,omitempty"`
	Raw         string `json:"raw"`
}

func authorizedKeysPath(home string) string {
	return filepath.Join(home, ".ssh", "authorized_keys")
}

func readAuthorizedKeys(path string) ([]SSHKey, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	keys := []SSHKey{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 8192), 1<<20)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, err := parseAuthorizedKey(line)
		if err != nil {
			continue
		}
		key.Line = lineNo
		keys = append(keys, *key)
	}
	return keys, sc.Err()
}

func parseAuthorizedKey(line string) (*SSHKey, error) {
	pub, comment, options, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return nil, err
	}
	k := &SSHKey{
		Type:        pub.Type(),
		Comment:     comment,
		Fingerprint: ssh.FingerprintSHA256(pub),
		Raw:         line,
	}
	if len(options) > 0 {
		k.Options = strings.Join(options, ",")
	}
	return k, nil
}

// ValidatePublicKey rejects anything that is not a well-formed public key
// before it is written. An authorized_keys file with a malformed line makes
// sshd ignore it, which would silently lock the operator out.
func ValidatePublicKey(raw string) (*SSHKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("key is empty")
	}
	if strings.Contains(raw, "\n") {
		return nil, fmt.Errorf("paste a single public key, not a file with several lines")
	}
	if strings.Contains(raw, "PRIVATE KEY") {
		return nil, fmt.Errorf("that is a private key — paste the matching .pub file instead")
	}
	return parseAuthorizedKey(raw)
}

func (s *Service) ListKeys(username string) ([]SSHKey, string, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, "", ErrNotFound
	}
	path := authorizedKeysPath(u.HomeDir)
	keys, err := readAuthorizedKeys(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []SSHKey{}, path, nil
		}
		return nil, path, err
	}
	return keys, path, nil
}

// AddKey appends a key, creating ~/.ssh with the permissions sshd insists on.
// sshd refuses to read an authorized_keys file that is group- or
// world-writable, so getting these modes right is functional, not cosmetic.
func (s *Service) AddKey(username, raw string) (*SSHKey, error) {
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	key, err := ValidatePublicKey(raw)
	if err != nil {
		return nil, err
	}
	u, err := user.Lookup(username)
	if err != nil {
		return nil, ErrNotFound
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	sshDir := filepath.Join(u.HomeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return nil, err
	}
	os.Chown(sshDir, uid, gid)
	os.Chmod(sshDir, 0o700)

	path := authorizedKeysPath(u.HomeDir)
	existing, _ := readAuthorizedKeys(path)
	for _, e := range existing {
		if e.Fingerprint == key.Fingerprint {
			return nil, fmt.Errorf("this key is already authorised for %s", username)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.WriteString(strings.TrimSpace(raw) + "\n"); err != nil {
		return nil, err
	}
	os.Chown(path, uid, gid)
	os.Chmod(path, 0o600)
	key.Line = len(existing) + 1
	return key, nil
}

// RemoveKey deletes by fingerprint rather than by line number, so a concurrent
// edit cannot cause the wrong key to be removed.
func (s *Service) RemoveKey(username, fingerprint string) error {
	if err := ValidateUsername(username); err != nil {
		return err
	}
	u, err := user.Lookup(username)
	if err != nil {
		return ErrNotFound
	}
	path := authorizedKeysPath(u.HomeDir)
	keys, err := readAuthorizedKeys(path)
	if err != nil {
		return err
	}
	kept := make([]string, 0, len(keys))
	found := false
	for _, k := range keys {
		if k.Fingerprint == fingerprint {
			found = true
			continue
		}
		kept = append(kept, k.Raw)
	}
	if !found {
		return fmt.Errorf("no key with fingerprint %s is authorised for %s", fingerprint, username)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".authorized_keys-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	content := ""
	if len(kept) > 0 {
		content = strings.Join(kept, "\n") + "\n"
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	os.Chown(tmp.Name(), uid, gid)
	return os.Rename(tmp.Name(), path)
}
