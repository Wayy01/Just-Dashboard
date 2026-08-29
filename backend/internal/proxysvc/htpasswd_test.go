package proxysvc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// The site form has a password-file field and, until now, nothing to put in
// it. These pin the file format, because nginx reads it and a malformed line
// is a site nobody can get into.

func TestSetAuthUserRejectsBadInput(t *testing.T) {
	cases := []struct{ file, user, password string }{
		{"../../etc/passwd", "admin", "correcthorse"},
		{"Site With Spaces", "admin", "correcthorse"},
		{"staging", "", "correcthorse"},
		{"staging", "ad:min", "correcthorse"},
		{"staging", "admin", "short"},
		{"staging", "admin", strings.Repeat("a", 73)},
	}
	svc := New(t.TempDir(), filepath.Join(t.TempDir(), "Caddyfile"))
	for _, tc := range cases {
		if _, err := svc.SetAuthUser(tc.file, tc.user, tc.password); err == nil {
			t.Errorf("accepted %q/%q", tc.file, tc.user)
		}
	}
}

// A colon in the user name silently truncates the entry, because the file
// format is user:hash.
func TestAuthUserPattern(t *testing.T) {
	for _, ok := range []string{"admin", "deploy.bot", "ci-runner", "a@example.com"} {
		if !authUserRe.MatchString(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "ad:min", "with space", "a\nb", strings.Repeat("a", 65)} {
		if authUserRe.MatchString(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

// bcrypt rather than a shell to htpasswd: apache2-utils is not installed on a
// host running nginx, and the password would become an argv that
// /proc/*/cmdline makes world-readable.
func TestHashesAreBcryptAndVerify(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct horse battery"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(hash), "$2") {
		t.Fatalf("not a bcrypt hash: %q", hash)
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte("correct horse battery")); err != nil {
		t.Fatalf("the hash does not verify: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte("wrong")); err == nil {
		t.Fatal("the wrong password verified")
	}
}

func TestRemoveAuthUserRejectsBadFileNames(t *testing.T) {
	svc := New(t.TempDir(), filepath.Join(t.TempDir(), "Caddyfile"))
	if _, err := svc.RemoveAuthUser("../x", "admin"); err == nil {
		t.Error("accepted a path as a file name")
	}
	if err := svc.DeleteAuthFile("../x"); err == nil {
		t.Error("accepted a path as a file name")
	}
}

// The password file lives under the configured nginx directory, not under a
// hard-coded /etc/nginx: JD_NGINX_DIR exists for the hosts whose nginx is
// somewhere else, and a file written where that nginx never looks is a site
// that refuses every visitor with a 403.
func TestAuthFilesLiveUnderTheConfiguredNginxDir(t *testing.T) {
	dir := t.TempDir()
	svc := New(dir, filepath.Join(t.TempDir(), "Caddyfile"))
	file, err := svc.SetAuthUser("staging", "admin", "correcthorsebattery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(file.Path, dir+string(os.PathSeparator)) {
		t.Fatalf("wrote outside the configured nginx dir: %s", file.Path)
	}
	if files := svc.ListAuthFiles(); len(files) != 1 || files[0].Name != "staging" {
		t.Fatalf("listing did not find it back: %+v", files)
	}
	// 0640 is only safe once the group is nginx's; where that account cannot
	// be resolved the file has to stay readable or the login never passes.
	st, err := os.Stat(file.Path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := st.Mode().Perm(); mode != 0o640 && mode != 0o644 {
		t.Fatalf("mode %o is neither group- nor world-readable", mode)
	}
}
