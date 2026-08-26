package dbx

import "strings"

// Recognising a database server that is not in a container.
//
// The Docker half of detect.go can read a container's environment, so it knows
// the credentials and can finish the job. Nothing here can: a Postgres that
// apt installed keeps its passwords in its own catalogue and its rules in
// pg_hba.conf, and no amount of reading the process tells this dashboard what
// to send. Guessing is not an option either — a wrong password against the
// operator's own server is an authentication failure in their logs and, on a
// host with fail2ban, a step towards banning this dashboard.
//
// So the answer here is narrower and honest: say the server is there, say what
// it is and where, and ask for the one thing that genuinely cannot be known.
// That is still the whole of the fix, because the failure it replaces is a
// Databases page that showed a server's containerised neighbours and stayed
// silent about the one running natively beside them.
//
// The signal is the *process*, not the port. A Postgres moved to 5433 is
// ordinary and a stranger on 5432 is not a Postgres, so matching a port would
// be wrong in both directions; the listening process is what the machine
// actually states about itself.

// Source says where a candidate was found, because the two are adopted
// differently and it is the difference the operator sees.
const (
	SourceDocker = "docker"
	SourceHost   = "host"
)

// HostListener is one listening socket, in the terms this package needs. The
// caller supplies them — proxysvc already enumerates sockets joined to their
// processes, and dbx has no business opening /proc.
type HostListener struct {
	Protocol string
	Address  string
	Port     int
	Process  string
	User     string
}

type hostRule struct {
	driver Driver
	// procs are matched as prefixes of the process name, because Linux
	// truncates the comm field at 15 characters: "clickhouse-server" is only
	// ever seen here as "clickhouse-serv".
	procs    []string
	user     string
	database string
	// openByDefault marks an engine that ships accepting connections with no
	// credentials at all. Those can simply be tried; the rest are asked about.
	openByDefault bool
}

var hostRules = []hostRule{
	{driver: DriverPostgres, procs: []string{"postgres", "postmaster"}, user: "postgres", database: "postgres"},
	// The MySQL DSN takes an empty database happily, and picking one the
	// server may not have would turn a working connection into a failing one.
	{driver: DriverMySQL, procs: []string{"mysqld", "mariadbd"}, user: "root"},
	{driver: DriverMongo, procs: []string{"mongod"}, database: "admin", openByDefault: true},
	{driver: DriverRedis, procs: []string{"redis-server", "valkey-server", "keydb-server"}, database: "0", openByDefault: true},
	{driver: DriverClickHouse, procs: []string{"clickhouse"}, user: "default", database: "default", openByDefault: true},
	{driver: DriverMSSQL, procs: []string{"sqlservr"}, user: "sa", database: "master"},
	// Oracle's listener is the process that owns the socket; the database
	// itself is behind it and always wants credentials.
	{driver: DriverOracle, procs: []string{"tnslsnr"}, user: "system"},
}

// DetectHost recognises a database server from a listening socket, returning
// nil for a socket that is not one this dashboard knows how to speak to.
//
// A container's published port is deliberately not matched here: the host sees
// it as `docker-proxy`, which is in no rule, so the Docker half stays the one
// place a container is described. A container on the host's own network
// namespace is the exception — it looks exactly like a native server, which is
// why the caller de-duplicates by address before adopting anything.
func DetectHost(l HostListener) *Candidate {
	if l.Protocol != "" && !strings.EqualFold(l.Protocol, "tcp") {
		return nil
	}
	if l.Port <= 0 {
		return nil
	}
	rule, ok := hostRuleFor(l.Process)
	if !ok {
		return nil
	}
	return &Candidate{
		Driver:           rule.driver,
		Source:           SourceHost,
		Process:          l.Process,
		Host:             hostAddress(l.Address),
		Port:             l.Port,
		User:             rule.user,
		Database:         rule.database,
		NeedsCredentials: !rule.openByDefault,
	}
}

func hostRuleFor(process string) (hostRule, bool) {
	name := strings.ToLower(strings.TrimSpace(process))
	if name == "" {
		return hostRule{}, false
	}
	for _, rule := range hostRules {
		for _, want := range rule.procs {
			if strings.HasPrefix(name, want) {
				return rule, true
			}
		}
	}
	return hostRule{}, false
}

// HostConnectionName is what a server found this way is called in the picker.
//
// The process name would be the obvious choice and is the wrong one: "mysqld"
// and "postmaster" name the program rather than the thing being connected to,
// and a list mixing them with container names reads as a list of unrelated
// objects. Saying where it is instead is what distinguishes it from the
// container next to it running the same engine.
func HostConnectionName(c Candidate) string {
	return string(c.Driver) + " on this host"
}
