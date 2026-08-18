package procs

import "testing"

// The prose in a packaged /etc/cron.d file is what broke the old parser: any
// comment six words or longer parsed as a five-field schedule plus a command,
// so documentation rendered as if it were a scheduled job.
func TestParseCrontabKeepsProseOutOfJobs(t *testing.T) {
	const content = `# Unlike any other crontab you don't have to run the ` + "`crontab`" + `
# command to install the new version when you edit this file
# and files in /etc/cron.d. These files also have username fields.
17 *	* * *	root	cd / && run-parts --report /etc/cron.hourly
25 6	* * *	root	test -x /usr/sbin/anacron || { cd / && run-parts --report /etc/cron.daily; }
`
	ct := parseCrontab(content, true)
	if len(ct.Jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d: %+v", len(ct.Jobs), ct.Jobs)
	}
	if len(ct.Comment) != 3 {
		t.Fatalf("expected 3 comment lines, got %d: %+v", len(ct.Comment), ct.Comment)
	}
	first := ct.Jobs[0]
	if first.Schedule != "17 * * * *" {
		t.Errorf("schedule = %q, want %q", first.Schedule, "17 * * * *")
	}
	if first.User != "root" {
		t.Errorf("user = %q, want root", first.User)
	}
	if first.Command != "cd / && run-parts --report /etc/cron.hourly" {
		t.Errorf("command = %q", first.Command)
	}
}

// A personal crontab has no username column, so the field after the schedule
// belongs to the command.
func TestParseCrontabUserDialect(t *testing.T) {
	ct := parseCrontab("*/5 * * * * /usr/bin/backup.sh --now\n", false)
	if len(ct.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(ct.Jobs))
	}
	if ct.Jobs[0].User != "" {
		t.Errorf("user = %q, want empty for a personal crontab", ct.Jobs[0].User)
	}
	if ct.Jobs[0].Command != "/usr/bin/backup.sh --now" {
		t.Errorf("command = %q", ct.Jobs[0].Command)
	}
}

// A commented-out schedule is a disabled job; a commented-out sentence is not.
func TestParseCrontabDisabledJob(t *testing.T) {
	ct := parseCrontab("# 30 4 * * * /usr/bin/rotate.sh\n# this one is just a note about it\n", false)
	if len(ct.Jobs) != 1 {
		t.Fatalf("expected 1 disabled job, got %d: %+v", len(ct.Jobs), ct.Jobs)
	}
	if !ct.Jobs[0].Disabled {
		t.Error("job should be marked disabled")
	}
	if len(ct.Comment) != 1 {
		t.Errorf("expected the note to stay a comment, got %+v", ct.Comment)
	}
}

func TestIsCronField(t *testing.T) {
	valid := []string{"*", "5", "*/5", "5-55/10", "1,2,3", "jan", "MON", "mon-fri", "0"}
	for _, f := range valid {
		if !isCronField(f) {
			t.Errorf("isCronField(%q) = false, want true", f)
		}
	}
	invalid := []string{"Unlike", "don't", "crontab", "", "run-parts", "hello,world"}
	for _, f := range invalid {
		if isCronField(f) {
			t.Errorf("isCronField(%q) = true, want false", f)
		}
	}
}

func TestValidateCrontabRejectsProse(t *testing.T) {
	if err := ValidateCrontab("this is not a cron line at all really\n"); err == nil {
		t.Error("expected prose to be rejected")
	}
	if err := ValidateCrontab("*/5 * * * * /usr/bin/backup.sh\n"); err != nil {
		t.Errorf("valid entry rejected: %v", err)
	}
}
