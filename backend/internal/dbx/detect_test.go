package dbx

import (
	"encoding/json"
	"strings"
	"testing"
)

func ports(containerPort, hostPort int) []PublishedPort {
	return []PublishedPort{{ContainerPort: containerPort, HostIP: "127.0.0.1", HostPort: hostPort}}
}

// The DSN built from a container's own statement of itself has to be one the
// driver accepts, because the operator never sees it: it is sealed on the way
// in, and a wrong one surfaces much later as a login failure they cannot
// debug from the UI.
func TestDetectBuildsAUsableDSN(t *testing.T) {
	cases := []struct {
		name      string
		image     string
		env       map[string]string
		published []PublishedPort
		driver    Driver
		dsn       string
	}{
		{
			name: "postgres", image: "postgres:16",
			env:       map[string]string{"POSTGRES_USER": "jdtest", "POSTGRES_PASSWORD": "jdtest", "POSTGRES_DB": "shop"},
			published: ports(5432, 5432), driver: DriverPostgres,
			dsn: "postgres://jdtest:jdtest@127.0.0.1:5432/shop?sslmode=disable",
		},
		{
			// POSTGRES_DB defaults to the user's name, not to "postgres".
			name: "postgres without a database", image: "postgres:16-alpine",
			env:       map[string]string{"POSTGRES_USER": "app", "POSTGRES_PASSWORD": "pw"},
			published: ports(5432, 5432), driver: DriverPostgres,
			dsn: "postgres://app:pw@127.0.0.1:5432/app?sslmode=disable",
		},
		{
			name: "mysql prefers the unprivileged account", image: "mysql:8",
			env: map[string]string{
				"MYSQL_ROOT_PASSWORD": "rootpw", "MYSQL_USER": "app",
				"MYSQL_PASSWORD": "apppw", "MYSQL_DATABASE": "shop",
			},
			published: ports(3306, 3306), driver: DriverMySQL,
			dsn: "app:apppw@tcp(127.0.0.1:3306)/shop",
		},
		{
			name: "mysql falls back to root", image: "mysql:8",
			env:       map[string]string{"MYSQL_ROOT_PASSWORD": "rootpw", "MYSQL_DATABASE": "shop"},
			published: ports(3306, 3306), driver: DriverMySQL,
			dsn: "root:rootpw@tcp(127.0.0.1:3306)/shop",
		},
		{
			name: "mariadb", image: "mariadb:11",
			env:       map[string]string{"MARIADB_ROOT_PASSWORD": "pw", "MARIADB_DATABASE": "shop"},
			published: ports(3306, 3307), driver: DriverMySQL,
			dsn: "root:pw@tcp(127.0.0.1:3307)/shop",
		},
		{
			name: "redis with no password at all", image: "redis:7",
			published: ports(6379, 6379), driver: DriverRedis,
			dsn: "redis://127.0.0.1:6379/0",
		},
		{
			name: "redis with a password", image: "redis:7-alpine",
			env:       map[string]string{"REDIS_PASSWORD": "s3cret"},
			published: ports(6379, 6379), driver: DriverRedis,
			dsn: "redis://:s3cret@127.0.0.1:6379/0",
		},
		{
			name: "mongo", image: "mongo:7",
			env:       map[string]string{"MONGO_INITDB_ROOT_USERNAME": "root", "MONGO_INITDB_ROOT_PASSWORD": "pw"},
			published: ports(27017, 27017), driver: DriverMongo,
			dsn: "mongodb://root:pw@127.0.0.1:27017/admin",
		},
		{
			name: "sql server", image: "mcr.microsoft.com/mssql/server:2022-latest",
			env:       map[string]string{"MSSQL_SA_PASSWORD": "JdTest#2024pw"},
			published: ports(1433, 1433), driver: DriverMSSQL,
			dsn: "sqlserver://sa:JdTest%232024pw@127.0.0.1:1433?database=master",
		},
		{
			// The native port, not the 8123 HTTP one the same image publishes.
			name: "clickhouse", image: "clickhouse/clickhouse-server:24.8",
			published: []PublishedPort{
				{ContainerPort: 8123, HostIP: "127.0.0.1", HostPort: 8123},
				{ContainerPort: 9000, HostIP: "127.0.0.1", HostPort: 9000},
			},
			driver: DriverClickHouse,
			dsn:    "clickhouse://default@127.0.0.1:9000/default",
		},
		{
			name: "oracle prefers the application account", image: "gvenzl/oracle-free:23-slim-faststart",
			env:       map[string]string{"ORACLE_PASSWORD": "sys", "APP_USER": "jdtest", "APP_USER_PASSWORD": "jdtest"},
			published: ports(1521, 1521), driver: DriverOracle,
			dsn: "oracle://jdtest:jdtest@127.0.0.1:1521/FREEPDB1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, password := Detect("db", c.image, c.env, c.published)
			if got == nil {
				t.Fatalf("image %q was not recognised", c.image)
			}
			if got.Driver != c.driver {
				t.Errorf("driver = %q, want %q", got.Driver, c.driver)
			}
			if !got.Connectable() {
				t.Fatalf("not connectable: %s", got.Reason)
			}
			if dsn := BuildDSN(*got, password); dsn != c.dsn {
				t.Errorf("dsn  = %q\nwant = %q", dsn, c.dsn)
			}
		})
	}
}

// A wrong guess does not fail cleanly — it saves a connection that looks real
// and then refuses to open — so anything not recognised is left alone.
func TestDetectIgnoresWhatItDoesNotKnow(t *testing.T) {
	for _, image := range []string{
		"nginx:alpine",
		"acme/postgres-backup:1", // not postgres, despite the name
		"mycorp/redis-exporter",  // an exporter is not the server
		"caddy:2-alpine",
		"bet-bot-high-market-tracker",
	} {
		if got, _ := Detect("c", image, nil, ports(5432, 5432)); got != nil {
			t.Errorf("image %q was taken for a %s", image, got.Driver)
		}
	}
	// And the registry-with-a-port case must not be mistaken for a tag.
	if got, _ := Detect("c", "registry.example.com:5000/postgres:16", nil, ports(5432, 5432)); got == nil {
		t.Error("a private-registry postgres should still be recognised")
	}
}

// A container with nothing published is running and unreachable from here.
// Saying so beats omitting it: it is the commonest reason a database somebody
// can see in the Docker page is one they cannot connect to.
func TestDetectExplainsAnUnreachableContainer(t *testing.T) {
	got, _ := Detect("db", "postgres:16", map[string]string{"POSTGRES_PASSWORD": "pw"}, nil)
	if got == nil {
		t.Fatal("a postgres with no published port should still be reported")
	}
	if got.Connectable() {
		t.Error("a container with no published port is not connectable")
	}
	if got.Reason == "" {
		t.Error("it should say why")
	}
}

// A port published to every interface is dialled on loopback: it is the same
// machine, and that keeps the connection off the network.
func TestDetectDialsLoopback(t *testing.T) {
	got, _ := Detect("db", "postgres:16", nil,
		[]PublishedPort{{ContainerPort: 5432, HostIP: "0.0.0.0", HostPort: 5432}})
	if got.Host != "127.0.0.1" {
		t.Errorf("host = %q, want 127.0.0.1", got.Host)
	}
}

// The description handed to a browser must never carry the secret.
func TestCandidateCarriesNoPassword(t *testing.T) {
	got, password := Detect("db", "postgres:16",
		map[string]string{"POSTGRES_USER": "u", "POSTGRES_PASSWORD": "hunter2"}, ports(5432, 5432))
	if password != "hunter2" {
		t.Fatalf("password = %q", password)
	}
	blob, err := jsonMarshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(blob, "hunter2") {
		t.Errorf("the password reached the serialised candidate: %s", blob)
	}
}

func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
