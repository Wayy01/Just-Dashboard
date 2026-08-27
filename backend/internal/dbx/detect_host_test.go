package dbx

import "testing"

// The claims this makes about a machine are as easy to get backwards as any
// check, and two of them are load-bearing: a container's published port must
// not be read as a native server, and a process name arrives truncated.
func TestDetectHost(t *testing.T) {
	cases := []struct {
		name     string
		listener HostListener
		want     Driver
		needs    bool
	}{
		{
			name:     "postgres on a port that is not the default",
			listener: HostListener{Protocol: "tcp", Address: "127.0.0.1", Port: 5433, Process: "postgres"},
			want:     DriverPostgres, needs: true,
		},
		{
			name:     "mariadb answers as mysql",
			listener: HostListener{Protocol: "tcp", Address: "0.0.0.0", Port: 3306, Process: "mariadbd"},
			want:     DriverMySQL, needs: true,
		},
		{
			// Linux truncates comm at 15 characters, so this is the only
			// spelling this process ever sees.
			name:     "clickhouse, truncated by the kernel",
			listener: HostListener{Protocol: "tcp", Address: "127.0.0.1", Port: 9000, Process: "clickhouse-serv"},
			want:     DriverClickHouse, needs: false,
		},
		{
			name:     "redis ships open",
			listener: HostListener{Protocol: "tcp", Address: "127.0.0.1", Port: 6379, Process: "redis-server"},
			want:     DriverRedis, needs: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectHost(tc.listener)
			if got == nil {
				t.Fatal("not recognised")
			}
			if got.Driver != tc.want {
				t.Errorf("driver = %q, want %q", got.Driver, tc.want)
			}
			if got.Port != tc.listener.Port {
				t.Errorf("port = %d, want the one it is listening on (%d)", got.Port, tc.listener.Port)
			}
			if got.NeedsCredentials != tc.needs {
				t.Errorf("needsCredentials = %v, want %v", got.NeedsCredentials, tc.needs)
			}
			if got.Source != SourceHost {
				t.Errorf("source = %q, want %q", got.Source, SourceHost)
			}
		})
	}
}

// A container's published port belongs to the Docker half, which knows its
// credentials. Reading docker-proxy as a native server would report every
// containerised database twice — once connectable, once asking for a password.
func TestDetectHostIgnoresWhatIsNotADatabase(t *testing.T) {
	for _, l := range []HostListener{
		{Protocol: "tcp", Address: "0.0.0.0", Port: 5432, Process: "docker-proxy"},
		{Protocol: "tcp", Address: "0.0.0.0", Port: 5432, Process: ""},
		{Protocol: "udp", Address: "127.0.0.1", Port: 6379, Process: "redis-server"},
		{Protocol: "tcp", Address: "127.0.0.1", Port: 0, Process: "postgres"},
		{Protocol: "tcp", Address: "127.0.0.1", Port: 22, Process: "sshd"},
	} {
		if got := DetectHost(l); got != nil {
			t.Errorf("%+v was read as %s", l, got.Driver)
		}
	}
}

// A wildcard binding is dialled on loopback, for the same reason a published
// container port is: it is the same machine, and it keeps the connection off
// the network whatever the socket is bound to.
func TestDetectHostDialsLoopbackForAWildcardBinding(t *testing.T) {
	got := DetectHost(HostListener{Protocol: "tcp", Address: "0.0.0.0", Port: 5432, Process: "postgres"})
	if got == nil || got.Host != "127.0.0.1" {
		t.Fatalf("host = %v, want 127.0.0.1", got)
	}
	if name := HostConnectionName(*got); name != "postgres on this host" {
		t.Errorf("name = %q", name)
	}
}

// An engine bound to one IPv6 address is dialled at it, bracketed — an address
// a port cannot be joined to is the same as no address at all.
func TestDetectHostBracketsAnIPv6Address(t *testing.T) {
	got := DetectHost(HostListener{Protocol: "tcp", Address: "fd00::1", Port: 5432, Process: "postgres"})
	if got == nil || got.Host != "[fd00::1]" {
		t.Fatalf("host = %v, want [fd00::1]", got)
	}
	if dsn := BuildDSN(*got, "secret"); dsn != "postgres://postgres:secret@[fd00::1]:5432/postgres?sslmode=disable" {
		t.Errorf("dsn = %q", dsn)
	}
}
