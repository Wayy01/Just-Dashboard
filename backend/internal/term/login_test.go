package term

import (
	"os"
	"path/filepath"
	"testing"
)

// withPasswd points the account lookup at a fixture. resolveAccount reads the
// host's real /etc/passwd, which is exactly the file a test must not depend on.
func withPasswd(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "passwd")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	original := passwdFile
	passwdFile = path
	t.Cleanup(func() { passwdFile = original })
}

const fixture = `root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
postgres:x:114:120::/var/lib/postgresql:/bin/bash
ubuntu:x:1000:1000:Ubuntu:/home/ubuntu:/bin/bash
deploy:x:1001:1001::/home/deploy:/bin/zsh
locked:x:1002:1002::/home/locked:/usr/sbin/nologin
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
`

// The default has to be the account an operator would have reached by ssh:
// the login the provider created, not root and not a service account.
func TestLowestRegularAccountPicksTheProvisionedLogin(t *testing.T) {
	withPasswd(t, fixture)
	got, ok := lowestRegularAccount()
	if !ok {
		t.Fatal("no regular account found")
	}
	if got.Name != "ubuntu" {
		t.Fatalf("account = %q, want ubuntu", got.Name)
	}
	if got.Home != "/home/ubuntu" || got.Shell != "/bin/bash" {
		t.Fatalf("home/shell = %q/%q, want /home/ubuntu and /bin/bash", got.Home, got.Shell)
	}
	if got.UID != 1000 || got.GID != 1000 {
		t.Fatalf("uid/gid = %d/%d, want 1000/1000", got.UID, got.GID)
	}
}

// A service account with a real shell still must not be chosen: postgres here
// has /bin/bash but a system UID, and landing an operator in it would be both
// surprising and wrong.
func TestLowestRegularAccountSkipsSystemAndNologin(t *testing.T) {
	withPasswd(t, `root:x:0:0:root:/root:/bin/bash
postgres:x:114:120::/var/lib/postgresql:/bin/bash
svc:x:1000:1000::/home/svc:/usr/sbin/nologin
deploy:x:1005:1005::/home/deploy:/bin/sh
`)
	got, ok := lowestRegularAccount()
	if !ok {
		t.Fatal("no regular account found")
	}
	if got.Name != "deploy" {
		t.Fatalf("account = %q, want deploy (svc is nologin, postgres is a system uid)", got.Name)
	}
}

// A box administered only as root has no regular account, and saying so lets
// the caller fall back deliberately rather than inventing one.
func TestLowestRegularAccountReportsWhenThereIsNone(t *testing.T) {
	withPasswd(t, "root:x:0:0:root:/root:/bin/bash\n")
	if _, ok := lowestRegularAccount(); ok {
		t.Fatal("found a regular account in a root-only passwd file")
	}
}

func TestLoginShellOfReadsTheAccountsChoice(t *testing.T) {
	withPasswd(t, fixture)
	if got := loginShellOf("deploy"); got != "/bin/zsh" {
		t.Fatalf("loginShellOf(deploy) = %q, want /bin/zsh", got)
	}
	if got := loginShellOf("absent"); got != "" {
		t.Fatalf("loginShellOf(absent) = %q, want empty", got)
	}
}

// The argument vector is the security-relevant part: options have to reach su
// rather than the shell, and nothing may be assembled into a string for a
// shell to re-parse.
func TestLoginArgv(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("argv depends on being root; su is not usable otherwise")
	}
	a := Account{Name: "ubuntu", UID: 1000, GID: 1000, Home: "/home/ubuntu", Shell: "/bin/bash"}

	got := a.loginArgv("")
	want := []string{"su", "-l", "ubuntu"}
	if !equal(got, want) {
		t.Fatalf("loginArgv() = %v, want %v", got, want)
	}

	// su hands anything after the username to the shell, so -s must come
	// before it or the shell receives a stray flag instead.
	got = a.loginArgv("/bin/zsh")
	want = []string{"su", "-s", "/bin/zsh", "-l", "ubuntu"}
	if !equal(got, want) {
		t.Fatalf("loginArgv(zsh) = %v, want %v", got, want)
	}

	// Already root: there is nobody to become, so the login shell is exec'd
	// directly and -l is what still makes it read the profile.
	root := Account{Name: "root", UID: 0, GID: 0, Home: "/root", Shell: "/bin/bash"}
	got = root.loginArgv("")
	want = []string{"/bin/bash", "-l"}
	if !equal(got, want) {
		t.Fatalf("root loginArgv() = %v, want %v", got, want)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
