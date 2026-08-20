package netsec

import (
	"bufio"
	"context"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// BanEvent is one ban or unban, read from fail2ban's own log.
//
// The jail status only ever lists the bans in force right now, and a ban
// expires — so the attack that filled the log at 04:00 and was banned for ten
// minutes leaves nothing behind by the time anyone looks. Rather than poll the
// jail and diff the set, which would invent events and miss every ban shorter
// than the polling interval, this reads the log fail2ban already writes. It is
// the record; the dashboard was simply not showing it.
type BanEvent struct {
	// Action is "ban" or "unban".
	Action string    `json:"action"`
	Jail   string    `json:"jail"`
	IP     string    `json:"ip"`
	At     time.Time `json:"at"`
}

// Where fail2ban writes by default. A host that logs only to the journal has
// no file here, which is reported as "no readable record" rather than as an
// error — it is a configuration, not a fault.
var fail2banLogs = []string{
	"/var/log/fail2ban.log",
	"/var/log/fail2ban.log.1",
}

// banLine matches the lines fail2ban writes when it acts. The format has been
// stable across releases:
//
//	2026-08-20 03:15:22,123 fail2ban.actions [1234]: NOTICE  [sshd] Ban 203.0.113.9
//
// Only Ban and Unban are taken. "Found" lines are one per failed attempt and
// would bury the actions in noise; failed attempts are btmp's answer, and the
// Security page reads that separately.
var banLine = regexp.MustCompile(
	`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})[,.]\d+\s+\S+\s+\[\d+\]:\s+\S+\s+\[([^\]]+)\]\s+(Ban|Unban)\s+(\S+)`)

// BanHistory returns recent ban activity, newest first.
func (s *Service) BanHistory(ctx context.Context, limit int) ([]BanEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	events := []BanEvent{}
	// Oldest rotation first, so that if a log is mid-rotation the ordering
	// still comes out right after the sort below.
	for i := len(fail2banLogs) - 1; i >= 0; i-- {
		if ctx.Err() != nil {
			break
		}
		found, err := readBanLog(fail2banLogs[i])
		if err != nil {
			continue
		}
		events = append(events, found...)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].At.After(events[j].At) })
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func readBanLog(path string) ([]BanEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []BanEvent
	scanner := bufio.NewScanner(f)
	// A single log line is short, but a corrupted file can present a very long
	// one; the default 64K limit would stop the scan there and silently
	// truncate the history.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if ev, ok := parseBanLine(scanner.Text()); ok {
			events = append(events, ev)
		}
	}
	return events, scanner.Err()
}

func parseBanLine(line string) (BanEvent, bool) {
	m := banLine.FindStringSubmatch(line)
	if m == nil {
		return BanEvent{}, false
	}
	at, err := time.ParseInLocation("2006-01-02 15:04:05", m[1], time.Local)
	if err != nil {
		return BanEvent{}, false
	}
	return BanEvent{
		Action: strings.ToLower(m[3]),
		Jail:   m[2],
		IP:     m[4],
		At:     at,
	}, true
}
