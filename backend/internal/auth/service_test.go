package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/store"
)

func TestLoginAlwaysStartsWithASecondFactorOutstanding(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sealer, err := NewSealer(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(st, sealer, time.Hour, time.Minute)
	user, err := svc.CreateUser(t.Context(), "tester", "Correct-Horse-Battery-9", RoleAdmin, false)
	if err != nil {
		t.Fatal(err)
	}

	login, err := svc.Login(t.Context(), user.Username, "Correct-Horse-Battery-9", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if !login.NeedsEnroll || login.NeedsTOTP {
		t.Fatalf("new account login = enroll:%v totp:%v, want enrollment only", login.NeedsEnroll, login.NeedsTOTP)
	}
	session, _, err := svc.ResolveSession(t.Context(), login.Token)
	if err != nil {
		t.Fatal(err)
	}
	if session.TwoFAPassed {
		t.Fatal("password-only login created an elevated session")
	}
	if !svc.Require2FA() {
		t.Fatal("service reported mandatory two-factor as optional")
	}
}
