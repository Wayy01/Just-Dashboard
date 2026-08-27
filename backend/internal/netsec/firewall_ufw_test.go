package netsec

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// A fake ufw, replying with output copied verbatim from a real one.
//
// The behaviour these pin is ufw's, not this package's, and it is behaviour
// nobody would guess: `ufw status numbered` prints a dual-stack rule twice,
// `ufw delete <n>` removes exactly one of the two, and `ufw allow` answers a
// duplicate by printing "Skipping" and exiting zero.
type fakeUFW struct {
	listing string
	calls   []string
	replies map[string]string
}

func (f *fakeUFW) install(t *testing.T) {
	t.Helper()
	previous := run
	run = func(_ context.Context, name string, args ...string) (string, error) {
		call := name + " " + strings.Join(args, " ")
		f.calls = append(f.calls, call)
		switch {
		case strings.HasPrefix(call, "ufw status numbered"):
			return f.listing, nil
		case strings.HasPrefix(call, "ufw status verbose"):
			return "Status: active\nLogging: on (low)\nDefault: deny (incoming), allow (outgoing), disabled (routed)\n", nil
		case strings.HasPrefix(call, "ufw --force delete "):
			// A delete renumbers everything below it, which is the reason the
			// twin cannot be found by arithmetic either. Modelled so the
			// numbers these tests assert are the ones a real ufw would print.
			n, err := strconv.Atoi(args[len(args)-1])
			if err != nil {
				return "", err
			}
			f.listing = deleteNumberedLine(f.listing, n)
			return "Rule deleted", nil
		}
		if reply, ok := f.replies[call]; ok {
			return reply, nil
		}
		return "Rule added", nil
	}
	t.Cleanup(func() { run = previous })
}

// deleteNumberedLine drops one "[ n]" row and renumbers the rest, exactly as
// ufw's own listing does after a delete.
func deleteNumberedLine(listing string, number int) string {
	var out []string
	next := 1
	for _, line := range strings.Split(listing, "\n") {
		m := ufwNumberedRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			out = append(out, line)
			continue
		}
		if n, _ := strconv.Atoi(m[1]); n == number {
			continue
		}
		out = append(out, fmt.Sprintf("[%2d] %s", next, m[2]))
		next++
	}
	return strings.Join(out, "\n")
}

// The listing is a trimmed copy of a real `ufw status numbered` on a
// dual-stack host: the IPv4 rules first, then the same rules again with a
// "(v6)" suffix.
const ufwDualStackListing = `Status: active

     To                         Action      From
     --                         ------      ----
[ 1] 8000/tcp                   ALLOW IN    Anywhere
[ 2] OpenSSH                    ALLOW IN    Anywhere
[ 3] 80                         ALLOW IN    Anywhere
[ 4] 443/tcp                    ALLOW IN    Anywhere
[ 5] 8000/tcp (v6)              ALLOW IN    Anywhere (v6)
[ 6] OpenSSH (v6)               ALLOW IN    Anywhere (v6)
[ 7] 80 (v6)                    ALLOW IN    Anywhere (v6)
[ 8] 443/tcp (v6)               ALLOW IN    Anywhere (v6)
`

// Deleting a rule has to take the IPv6 copy with it. The page folds the "(v6)"
// duplicates away — eight rules must not read as sixteen — so a leftover half
// is not merely untidy: the rule list shows a closed port that is still open to
// every IPv6 client on the internet.
func TestUFWDeleteAlsoRemovesTheIPv6Copy(t *testing.T) {
	f := &fakeUFW{listing: ufwDualStackListing}
	f.install(t)
	if _, err := (ufwBackend{}).DeleteRule(t.Context(), 3); err != nil {
		t.Fatal(err)
	}
	var deletes []string
	for _, c := range f.calls {
		if strings.Contains(c, "delete") {
			deletes = append(deletes, c)
		}
	}
	want := []string{"ufw --force delete 3", "ufw --force delete 6"}
	if strings.Join(deletes, "; ") != strings.Join(want, "; ") {
		t.Fatalf("deletes = %v, want %v", deletes, want)
	}
}

// The IPv6 half of a rule is deleted on its own without looking for a twin of
// its own — it *is* the twin, and hunting for another would delete the IPv4
// rule the operator chose to keep.
func TestUFWDeletingTheIPv6HalfLeavesTheIPv4Rule(t *testing.T) {
	f := &fakeUFW{listing: ufwDualStackListing}
	f.install(t)
	if _, err := (ufwBackend{}).DeleteRule(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if c == "ufw --force delete 3" {
			t.Fatalf("deleted the IPv4 rule as well: %v", f.calls)
		}
	}
}

// A rule ufw only ever wrote into one table — anything with an IPv4 source —
// has no twin, and the delete must stop after the one entry that exists.
func TestUFWDeleteOfAV4OnlyRuleTouchesNothingElse(t *testing.T) {
	f := &fakeUFW{listing: `Status: active

     To                         Action      From
     --                         ------      ----
[ 1] 5432/tcp                   ALLOW IN    10.0.0.0/8
[ 2] 80 (v6)                    ALLOW IN    Anywhere (v6)
`}
	f.install(t)
	if _, err := (ufwBackend{}).DeleteRule(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	deletes := 0
	for _, c := range f.calls {
		if strings.Contains(c, "delete") {
			deletes++
		}
	}
	if deletes != 1 {
		t.Fatalf("%d deletes for a rule with no IPv6 copy: %v", deletes, f.calls)
	}
}

// "Skipping adding existing rule" is ufw's way of saying it did nothing, and
// it exits zero while saying it. Reported as a success it becomes an edit that
// deletes a rule and adds none.
func TestUFWAddReportsARuleItDeclinedToAdd(t *testing.T) {
	f := &fakeUFW{listing: ufwDualStackListing, replies: map[string]string{
		"ufw allow in from any to any port 80 proto tcp": "Skipping adding existing rule\nSkipping adding existing rule (v6)\n",
	}}
	f.install(t)
	_, err := (ufwBackend{}).AddRule(t.Context(), RuleRequest{
		Action: "allow", Direction: "in", Port: "80", Protocol: "tcp",
	})
	if err == nil {
		t.Fatal("an add that added nothing reported as success")
	}
}

// The other half of the same test: a rule whose IPv4 entry exists and whose
// IPv6 entry does not is a real add, and refusing it would leave the port
// closed over IPv6 with the page reporting it open.
func TestUFWAddAcceptsAPartialSkip(t *testing.T) {
	f := &fakeUFW{listing: ufwDualStackListing, replies: map[string]string{
		"ufw allow in from any to any port 80 proto tcp": "Skipping adding existing rule\nRule added (v6)\n",
	}}
	f.install(t)
	if _, err := (ufwBackend{}).AddRule(t.Context(), RuleRequest{
		Action: "allow", Direction: "in", Port: "80", Protocol: "tcp",
	}); err != nil {
		t.Fatalf("refused an add that did write a rule: %v", err)
	}
}

// The listing parser, against the real thing. The "(v6)" suffix lands on the
// destination or the source depending on the rule, and left in either field it
// makes one rule read as two different ones.
func TestUFWListingParsesTheRealFormat(t *testing.T) {
	f := &fakeUFW{listing: ufwDualStackListing}
	f.install(t)
	st, err := (ufwBackend{}).Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Rules) != 8 {
		t.Fatalf("%d rules, want 8", len(st.Rules))
	}
	if !st.Enabled {
		t.Error("an active firewall read as inactive")
	}
	v4, v6 := st.Rules[2], st.Rules[6]
	if v4.To != "80" || v4.IPv6 {
		t.Errorf("IPv4 rule = %+v", v4)
	}
	if v6.To != "80" || !v6.IPv6 || v6.From != "Anywhere" {
		t.Errorf("IPv6 rule = %+v", v6)
	}
	if !sameRule(v4, v6) {
		t.Error("the two halves of one rule do not compare equal, so a delete cannot find the twin")
	}
	if st.Policy.Incoming != "deny" || st.Logging != "on (low)" {
		t.Errorf("policy/logging not read from the verbose block: %+v %q", st.Policy, st.Logging)
	}
}

// `ufw allow 6379` is what somebody following a README types, and it writes a
// rule whose destination is a bare number. Everything the catalogue does hangs
// off the port, so a rule parsed without one is a Redis port open to the world
// with no name beside it and no warning — on the spelling most likely to
// produce exactly that mistake.
func TestUFWParsesAPortWrittenWithoutAProtocol(t *testing.T) {
	f := &fakeUFW{listing: `Status: active

     To                         Action      From
     --                         ------      ----
[ 1] 6379                       ALLOW IN    Anywhere
[ 2] OpenSSH                    ALLOW IN    Anywhere
[ 3] 8000:8010                  ALLOW IN    Anywhere
`}
	f.install(t)
	st, err := New().Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	redis := st.Rules[0]
	if redis.Port != "6379" {
		t.Fatalf("Port = %q, want 6379", redis.Port)
	}
	if redis.Service == "" {
		t.Error("the catalogue could not name a service it knows")
	}
	if redis.Danger == "" {
		t.Error("no warning on a datastore port open to everyone")
	}
	// An application profile is not a port and must not be read as one.
	if st.Rules[1].Port != "" {
		t.Errorf("profile rule read as port %q", st.Rules[1].Port)
	}
	if st.Rules[2].Port != "8000:8010" {
		t.Errorf("range = %q", st.Rules[2].Port)
	}
}
