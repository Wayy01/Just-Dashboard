package netsec

import (
	"testing"
	"time"
)

func at(day, hour int) time.Time {
	return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
}

func TestSummariseBansRanksPersistentOffenders(t *testing.T) {
	sum := SummariseBans([]BanEvent{
		{Action: "ban", Jail: "sshd", IP: "203.0.113.9", At: at(20, 1)},
		{Action: "ban", Jail: "sshd", IP: "203.0.113.9", At: at(21, 2)},
		{Action: "ban", Jail: "nginx", IP: "203.0.113.9", At: at(22, 3)},
		{Action: "ban", Jail: "sshd", IP: "198.51.100.7", At: at(22, 4)},
		{Action: "unban", Jail: "sshd", IP: "203.0.113.9", At: at(22, 5)},
	}, 10)

	if sum.Bans != 4 || sum.Unbans != 1 || sum.Total != 5 {
		t.Fatalf("counts = %d bans, %d unbans, %d total", sum.Bans, sum.Unbans, sum.Total)
	}
	if len(sum.Offenders) != 2 {
		t.Fatalf("got %d offenders", len(sum.Offenders))
	}
	top := sum.Offenders[0]
	if top.IP != "203.0.113.9" || top.Bans != 3 {
		t.Fatalf("top offender = %+v", top)
	}
	if len(top.Jails) != 2 {
		t.Errorf("an address caught by two jails should list both: %v", top.Jails)
	}
	if !top.First.Equal(at(20, 1)) || !top.Last.Equal(at(22, 3)) {
		t.Errorf("first/last = %v / %v", top.First, top.Last)
	}
}

// Every ban is eventually followed by an unban. Counting both would double
// every number and make an expired ban look like a second attack.
func TestSummariseBansIgnoresUnbansInTheTally(t *testing.T) {
	sum := SummariseBans([]BanEvent{
		{Action: "ban", Jail: "sshd", IP: "203.0.113.9", At: at(20, 1)},
		{Action: "unban", Jail: "sshd", IP: "203.0.113.9", At: at(20, 2)},
	}, 10)
	if sum.Offenders[0].Bans != 1 {
		t.Fatalf("bans = %d", sum.Offenders[0].Bans)
	}
	if sum.ByJail["sshd"] != 1 {
		t.Fatalf("byJail = %v", sum.ByJail)
	}
}

func TestSummariseBansBucketsByDay(t *testing.T) {
	sum := SummariseBans([]BanEvent{
		{Action: "ban", Jail: "sshd", IP: "a", At: at(20, 1)},
		{Action: "ban", Jail: "sshd", IP: "b", At: at(20, 9)},
		{Action: "ban", Jail: "sshd", IP: "c", At: at(22, 1)},
	}, 10)
	if len(sum.PerDay) != 2 {
		t.Fatalf("got %d days: %+v", len(sum.PerDay), sum.PerDay)
	}
	if sum.PerDay[0].Day != "2026-08-20" || sum.PerDay[0].Count != 2 {
		t.Fatalf("first bucket = %+v", sum.PerDay[0])
	}
	if sum.PerDay[1].Count != 1 {
		t.Fatalf("second bucket = %+v", sum.PerDay[1])
	}
}

func TestSummariseBansCapsTheList(t *testing.T) {
	events := []BanEvent{}
	for i := 0; i < 30; i++ {
		events = append(events, BanEvent{Action: "ban", Jail: "sshd", IP: string(rune('a' + i)), At: at(20, i%24)})
	}
	sum := SummariseBans(events, 5)
	if len(sum.Offenders) != 5 {
		t.Fatalf("got %d, want the list capped at 5", len(sum.Offenders))
	}
}

func TestSummariseBansHandlesNothing(t *testing.T) {
	sum := SummariseBans(nil, 10)
	if sum.Bans != 0 || len(sum.Offenders) != 0 || sum.Since != nil {
		t.Fatalf("empty summary = %+v", sum)
	}
}
