package netsec

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Jail struct {
	Name          string   `json:"name"`
	Currently     int      `json:"currentlyFailed"`
	TotalFailed   int      `json:"totalFailed"`
	CurrentlyBan  int      `json:"currentlyBanned"`
	TotalBanned   int      `json:"totalBanned"`
	BannedIPs     []string `json:"bannedIps"`
	FileList      []string `json:"fileList"`
}

type Fail2banStatus struct {
	Available bool   `json:"available"`
	Running   bool   `json:"running"`
	Jails     []Jail `json:"jails"`
	Error     string `json:"error,omitempty"`
}

func (s *Service) Fail2banAvailable() bool {
	_, err := exec.LookPath("fail2ban-client")
	return err == nil
}

// Fail2banStatus queries fail2ban-client. Its output is a fixed-shape text
// report rather than JSON, so it is parsed by the labels it prints.
func (s *Service) Fail2banStatus(ctx context.Context) (*Fail2banStatus, error) {
	st := &Fail2banStatus{Jails: []Jail{}}
	if !s.Fail2banAvailable() {
		return st, nil
	}
	st.Available = true
	out, err := run(ctx, "fail2ban-client", "status")
	if err != nil {
		st.Error = err.Error()
		return st, nil
	}
	st.Running = true
	names := parseJailList(out)
	for _, name := range names {
		jail, err := s.jailStatus(ctx, name)
		if err != nil {
			continue
		}
		st.Jails = append(st.Jails, *jail)
	}
	sort.Slice(st.Jails, func(i, j int) bool { return st.Jails[i].Name < st.Jails[j].Name })
	return st, nil
}

func parseJailList(out string) []string {
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "Jail list:") {
			continue
		}
		_, after, _ := strings.Cut(line, "Jail list:")
		names := []string{}
		for _, n := range strings.Split(after, ",") {
			if n = strings.TrimSpace(n); n != "" {
				names = append(names, n)
			}
		}
		return names
	}
	return nil
}

var jailNameRe = regexp.MustCompile(`^[A-Za-z0-9._\-]{1,64}$`)

func (s *Service) jailStatus(ctx context.Context, name string) (*Jail, error) {
	if !jailNameRe.MatchString(name) {
		return nil, fmt.Errorf("invalid jail name %q", name)
	}
	out, err := run(ctx, "fail2ban-client", "status", name)
	if err != nil {
		return nil, err
	}
	j := &Jail{Name: name, BannedIPs: []string{}, FileList: []string{}}
	for _, line := range strings.Split(out, "\n") {
		label, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch {
		case strings.Contains(label, "Currently failed"):
			j.Currently = atoi(value)
		case strings.Contains(label, "Total failed"):
			j.TotalFailed = atoi(value)
		case strings.Contains(label, "Currently banned"):
			j.CurrentlyBan = atoi(value)
		case strings.Contains(label, "Total banned"):
			j.TotalBanned = atoi(value)
		case strings.Contains(label, "Banned IP list"):
			j.BannedIPs = strings.Fields(value)
		case strings.Contains(label, "File list"):
			j.FileList = strings.Fields(value)
		}
	}
	return j, nil
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// Unban releases one address from a jail. Both arguments are validated: the
// jail name against a name pattern and the address by actually parsing it as
// an IP, so neither can carry anything but what it claims to be.
func (s *Service) Unban(ctx context.Context, jail, ip string) (string, error) {
	if !jailNameRe.MatchString(jail) {
		return "", fmt.Errorf("invalid jail name %q", jail)
	}
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("%q is not a valid IP address", ip)
	}
	return run(ctx, "fail2ban-client", "set", jail, "unbanip", ip)
}

func (s *Service) Ban(ctx context.Context, jail, ip string) (string, error) {
	if !jailNameRe.MatchString(jail) {
		return "", fmt.Errorf("invalid jail name %q", jail)
	}
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("%q is not a valid IP address", ip)
	}
	return run(ctx, "fail2ban-client", "set", jail, "banip", ip)
}
