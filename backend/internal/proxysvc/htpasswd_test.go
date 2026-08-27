package proxysvc

import (
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
	for _, tc := range cases {
		if _, err := SetAuthUser(tc.file, tc.user, tc.password); err == nil {
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
	if _, err := RemoveAuthUser("../x", "admin"); err == nil {
		t.Error("accepted a path as a file name")
	}
	if err := DeleteAuthFile("../x"); err == nil {
		t.Error("accepted a path as a file name")
	}
}
