package dockerx

import "testing"

func TestIsSecretEnvKey(t *testing.T) {
	secret := []string{
		"VPSD_MASTER_KEY", "VPSD_BOOTSTRAP_PASSWORD", "AWS_SECRET_ACCESS_KEY",
		"API_TOKEN", "DB_PASSWD", "KEY", "GITHUB_TOKEN", "DATABASE_DSN",
	}
	for _, k := range secret {
		if !IsSecretEnvKey(k) {
			t.Errorf("IsSecretEnvKey(%q) = false, want true", k)
		}
	}
	plain := []string{"PATH", "HOME", "VPSD_ADDR", "LANG", "VPSD_LOG_LEVEL", "HOSTNAME"}
	for _, k := range plain {
		if IsSecretEnvKey(k) {
			t.Errorf("IsSecretEnvKey(%q) = true, want false", k)
		}
	}
}

func TestRedactEnv(t *testing.T) {
	in := []string{
		"VPSD_MASTER_KEY=a33fe23634b81b45354aa28a7c61acd1",
		"VPSD_ADDR=127.0.0.1:8080",
		"NOEQUALSHERE",
	}
	out := RedactEnv(in)
	if out[0] != "VPSD_MASTER_KEY="+RedactedEnvValue {
		t.Errorf("secret not redacted: %q", out[0])
	}
	if out[1] != in[1] {
		t.Errorf("plain value altered: %q", out[1])
	}
	if out[2] != in[2] {
		t.Errorf("malformed entry altered: %q", out[2])
	}
	// The original slice must not be modified in place: the same detail is
	// reused by callers that are allowed to see the real values.
	if in[0] == out[0] {
		t.Error("RedactEnv mutated its input")
	}
}
