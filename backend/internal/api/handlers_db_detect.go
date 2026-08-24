package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/dbx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/dockerx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
)

// Finding and making database servers, so nobody has to write a DSN by hand.
//
// The dashboard already drives the Docker socket and already knows how to run
// a container. An operator who wants a database was nonetheless being asked to
// go to the Docker page, start one, work out what its connection string would
// be from the environment variables they had just typed, come back, and paste
// it in. Every fact in that string was already in this process.
//
// Two routes close it. The first reports what is already running; the second
// starts something new. Both end at the same place — a connection row whose
// DSN was assembled here and sealed here.
//
// The password never crosses the wire in either direction. A detected server's
// is read from its container at the moment it is adopted; a provisioned one's
// is generated here and handed to the container, never to the browser. That is
// not decoration: `dockerx.RedactEnv` exists because container environment is
// where deployments keep their secrets, and a route that answered "here is the
// password for every database on this host" would undo it.

// detectedResponse is what the Databases page lists above the connections the
// operator has already made.
type detectedResponse struct {
	Servers []detectedServer `json:"servers"`
}

type detectedServer struct {
	dbx.Candidate
	// Adopted names the existing connection pointing at this container, so the
	// page can show "already connected" rather than offering to add a second
	// row for the same server.
	Adopted string `json:"adopted,omitempty"`
	Health  string `json:"health,omitempty"`
	Status  string `json:"status,omitempty"`
}

func (s *Server) handleDBDetected(w http.ResponseWriter, r *http.Request) error {
	httpx.SkipAudit(r)
	if s.modules.docker == nil {
		return httpx.Err(http.StatusServiceUnavailable, "docker_unavailable",
			"this host has no Docker socket, so there is nothing to detect")
	}
	ctx, cancel := timeoutCtx(r, 30*time.Second)
	defer cancel()

	containers, err := s.modules.docker.ListContainers(ctx, false)
	if err != nil {
		return httpx.Err(http.StatusBadGateway, "docker_failed", err.Error())
	}
	existing, err := s.existingDSNs(ctx)
	if err != nil {
		return err
	}

	out := detectedResponse{Servers: []detectedServer{}}
	for _, c := range containers {
		cand, _ := dbx.Detect(c.Name, c.Image, nil, publishedPorts(c.Ports))
		if cand == nil {
			continue
		}
		// The environment is only read once the image is known to be a
		// database, so an inspect is not paid for every container on the host.
		if detail, err := s.modules.docker.Inspect(ctx, c.ID); err == nil {
			cand, _ = dbx.Detect(c.Name, c.Image, envMap(detail.Env), publishedPorts(c.Ports))
		}
		if cand == nil {
			continue
		}
		out.Servers = append(out.Servers, detectedServer{
			Candidate: *cand,
			Adopted:   existing[addressKey(cand.Host, cand.Port)],
			Health:    c.Health,
			Status:    c.Status,
		})
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

// existingDSNs maps a host:port already covered by a saved connection to that
// connection's name. It opens each stored DSN, which is why it lives behind
// the same capability as the rest of this file.
func (s *Server) existingDSNs(ctx context.Context) (map[string]string, error) {
	rows, err := s.Store.DB.QueryContext(ctx, `SELECT id FROM db_connections`)
	if err != nil {
		return nil, httpx.Internal(err)
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, httpx.Internal(err)
		}
		ids = append(ids, id)
	}
	out := map[string]string{}
	for _, id := range ids {
		conn, _, err := s.dbConnRow(ctx, id)
		if err != nil {
			continue
		}
		out[addressKey(conn.Host, atoiDefault(conn.Port, 0))] = conn.Name
	}
	return out, nil
}

func addressKey(host string, port int) string { return host + ":" + strconv.Itoa(port) }

func publishedPorts(ports []dockerx.Port) []dbx.PublishedPort {
	out := make([]dbx.PublishedPort, 0, len(ports))
	for _, p := range ports {
		out = append(out, dbx.PublishedPort{
			ContainerPort: int(p.PrivatePort), HostIP: p.IP, HostPort: int(p.PublicPort),
		})
	}
	return out
}

func envMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, e := range env {
		if name, value, ok := strings.Cut(e, "="); ok {
			out[name] = value
		}
	}
	return out
}

type adoptRequest struct {
	Container string `json:"container"`
	Name      string `json:"name"`
}

// handleDBAdopt turns a detected server into a saved connection.
//
// The client names a container, not a DSN. That is the whole point: the
// credentials are read from the container here and sealed here, so the browser
// never holds them and an operator never types them.
func (s *Server) handleDBAdopt(w http.ResponseWriter, r *http.Request) error {
	var req adoptRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Container) == "" {
		return httpx.BadRequest("container is required")
	}
	if s.modules.docker == nil {
		return httpx.Err(http.StatusServiceUnavailable, "docker_unavailable",
			"this host has no Docker socket")
	}
	ctx, cancel := timeoutCtx(r, 30*time.Second)
	defer cancel()

	cand, password, err := s.candidateFor(ctx, req.Container)
	if err != nil {
		return err
	}
	if !cand.Connectable() {
		return httpx.BadRequest("%s cannot be connected to: %s", cand.Container, cand.Reason)
	}
	dsn := dbx.BuildDSN(*cand, password)
	if dsn == "" {
		return httpx.BadRequest("no connection string could be built for %s", cand.Driver)
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = cand.Container
	}
	if !connNameRe.MatchString(name) {
		return httpx.BadRequest("name may contain letters, digits, spaces, dots, dashes and underscores")
	}
	return s.saveConnection(w, r, name, cand.Driver, dsn, "database.connection.adopt",
		map[string]any{"container": cand.Container, "image": cand.Image})
}

// candidateFor re-detects one container by name, so an adopt acts on what is
// true now rather than on what a listing said some time ago.
func (s *Server) candidateFor(ctx context.Context, name string) (*dbx.Candidate, string, error) {
	containers, err := s.modules.docker.ListContainers(ctx, false)
	if err != nil {
		return nil, "", httpx.Err(http.StatusBadGateway, "docker_failed", err.Error())
	}
	for _, c := range containers {
		if c.Name != name && c.ID != name {
			continue
		}
		detail, err := s.modules.docker.Inspect(ctx, c.ID)
		if err != nil {
			return nil, "", httpx.Err(http.StatusBadGateway, "docker_failed", err.Error())
		}
		cand, password := dbx.Detect(c.Name, c.Image, envMap(detail.Env), publishedPorts(c.Ports))
		if cand == nil {
			return nil, "", httpx.BadRequest("%s is not a database image this dashboard recognises", name)
		}
		return cand, password, nil
	}
	return nil, "", httpx.BadRequest("no running container named %q", name)
}

// saveConnection is the tail every route that creates a connection shares:
// contain the DSN, seal it, store it, audit what it points at but never the
// string itself.
func (s *Server) saveConnection(
	w http.ResponseWriter, r *http.Request,
	name string, driver dbx.Driver, dsn string, action string, detail map[string]any,
) error {
	contained, err := s.containDSN(driver, dsn)
	if err != nil {
		return err
	}
	sealed, err := s.Sealer.Seal(contained)
	if err != nil {
		return httpx.Internal(err)
	}
	res, err := s.Store.DB.ExecContext(r.Context(),
		`INSERT INTO db_connections(name, driver, dsn_enc, created_at) VALUES(?,?,?,?)`,
		name, string(driver), sealed, time.Now().Unix())
	if err != nil {
		return httpx.BadRequest("could not save connection: %v", err)
	}
	id, _ := res.LastInsertId()
	conn, _, err := s.dbConnRow(r.Context(), id)
	if err != nil {
		return err
	}
	detail["driver"] = driver
	detail["host"] = conn.Host
	detail["user"] = conn.User
	httpx.SetAudit(r, action, name, detail)
	httpx.JSON(w, http.StatusCreated, conn)
	return nil
}

// --- provisioning -----------------------------------------------------------

// provisionTemplate is a database server this dashboard can start.
//
// Deliberately a short, closed list of the official images, and deliberately
// server-side: the frontend's Docker templates are starting points a person
// reads and edits, whereas these are run unattended and their settings have to
// be ones this package can also connect to afterwards. The port is where the
// engine listens; the volume is what stops the data disappearing with the
// container.
type provisionTemplate struct {
	driver   dbx.Driver
	label    string
	image    string
	port     int
	dataPath string
	// env builds the container's environment from a generated password.
	env func(password, database string) []dockerx.EnvVar
}

var provisionTemplates = map[string]provisionTemplate{
	"postgres": {
		driver: dbx.DriverPostgres, label: "PostgreSQL 16", image: "postgres:16-alpine",
		port: 5432, dataPath: "/var/lib/postgresql/data",
		env: func(pw, db string) []dockerx.EnvVar {
			return []dockerx.EnvVar{
				{Name: "POSTGRES_USER", Value: "jd"},
				{Name: "POSTGRES_PASSWORD", Value: pw},
				{Name: "POSTGRES_DB", Value: db},
			}
		},
	},
	"mysql": {
		driver: dbx.DriverMySQL, label: "MySQL 8", image: "mysql:8",
		port: 3306, dataPath: "/var/lib/mysql",
		env: func(pw, db string) []dockerx.EnvVar {
			return []dockerx.EnvVar{
				{Name: "MYSQL_ROOT_PASSWORD", Value: pw},
				{Name: "MYSQL_USER", Value: "jd"},
				{Name: "MYSQL_PASSWORD", Value: pw},
				{Name: "MYSQL_DATABASE", Value: db},
			}
		},
	},
	"mariadb": {
		driver: dbx.DriverMySQL, label: "MariaDB 11", image: "mariadb:11",
		port: 3306, dataPath: "/var/lib/mysql",
		env: func(pw, db string) []dockerx.EnvVar {
			return []dockerx.EnvVar{
				{Name: "MARIADB_ROOT_PASSWORD", Value: pw},
				{Name: "MARIADB_USER", Value: "jd"},
				{Name: "MARIADB_PASSWORD", Value: pw},
				{Name: "MARIADB_DATABASE", Value: db},
			}
		},
	},
	"redis": {
		driver: dbx.DriverRedis, label: "Redis 7", image: "redis:7-alpine",
		port: 6379, dataPath: "/data",
		env: func(pw, _ string) []dockerx.EnvVar {
			return []dockerx.EnvVar{{Name: "REDIS_PASSWORD", Value: pw}}
		},
	},
	"mongodb": {
		driver: dbx.DriverMongo, label: "MongoDB 7", image: "mongo:7",
		port: 27017, dataPath: "/data/db",
		env: func(pw, db string) []dockerx.EnvVar {
			return []dockerx.EnvVar{
				{Name: "MONGO_INITDB_ROOT_USERNAME", Value: "jd"},
				{Name: "MONGO_INITDB_ROOT_PASSWORD", Value: pw},
				{Name: "MONGO_INITDB_DATABASE", Value: db},
			}
		},
	},
}

type provisionOption struct {
	Engine string `json:"engine"`
	Label  string `json:"label"`
	Image  string `json:"image"`
	Driver string `json:"driver"`
}

func (s *Server) handleDBProvisionOptions(w http.ResponseWriter, r *http.Request) error {
	httpx.SkipAudit(r)
	// Ordered, because a map is not, and a list of engines that reshuffles on
	// every poll is unusable.
	out := []provisionOption{}
	for _, key := range []string{"postgres", "mysql", "mariadb", "redis", "mongodb"} {
		t := provisionTemplates[key]
		out = append(out, provisionOption{
			Engine: key, Label: t.label, Image: t.image, Driver: string(t.driver),
		})
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

type provisionRequest struct {
	Engine   string `json:"engine"`
	Name     string `json:"name"`
	Database string `json:"database"`
}

// handleDBProvision starts a database server and saves the connection to it.
//
// The password is generated here and never leaves this process except into the
// container's own environment: the operator does not choose it, see it or type
// it, which is the difference between "automatic" and "a form with fewer
// fields". It can always be read back from the container by an admin, and the
// dashboard's own copy is sealed like every other stored DSN.
//
// It publishes to loopback only. A database this dashboard started should not
// become reachable from the internet because a default was convenient, and an
// operator who wants otherwise can change the port binding on the Docker page,
// where that decision is visible.
func (s *Server) handleDBProvision(w http.ResponseWriter, r *http.Request) error {
	var req provisionRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	tmpl, ok := provisionTemplates[req.Engine]
	if !ok {
		return httpx.BadRequest("unknown engine %q", req.Engine)
	}
	if s.modules.docker == nil {
		return httpx.Err(http.StatusServiceUnavailable, "docker_unavailable",
			"this host has no Docker socket, so a server cannot be started")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "jd-" + req.Engine
	}
	if !containerNameRe.MatchString(name) {
		return httpx.BadRequest("a container name may contain letters, digits, dots, dashes and underscores")
	}
	database := strings.TrimSpace(req.Database)
	if database == "" {
		database = "app"
	}
	if !dbNameRe.MatchString(database) {
		return httpx.BadRequest("a database name may contain letters, digits and underscores")
	}

	password, err := generatePassword()
	if err != nil {
		return httpx.Internal(err)
	}
	// A pull on a slow link is the long part, and the engines here are small.
	ctx, cancel := timeoutCtx(r, 10*time.Minute)
	defer cancel()

	port, err := freeHostPort(tmpl.port)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	spec := dockerx.ContainerSpec{
		Name:  name,
		Image: tmpl.image,
		// Started, not merely created. A spec that only creates leaves a
		// container in "Created" that never listens, so the adopt that follows
		// waits for an engine that was never going to answer.
		Start:         true,
		RestartPolicy: "unless-stopped",
		Env:           tmpl.env(password, database),
		Ports: []dockerx.PortMapping{
			{HostIP: "127.0.0.1", HostPort: port, ContainerPort: tmpl.port, Protocol: "tcp"},
		},
		Mounts: []dockerx.MountSpec{
			{Type: "volume", Source: name + "-data", Target: tmpl.dataPath},
		},
	}
	if _, err := s.modules.docker.Create(ctx, spec, nil); err != nil {
		return httpx.BadRequest("could not start %s: %v", tmpl.label, err)
	}
	httpx.SetAudit(r, "database.server.provision", name,
		map[string]any{"engine": req.Engine, "image": tmpl.image, "port": port})

	// The container exists; whether the engine inside it is ready to be talked
	// to is a separate question, and the answer takes seconds to a minute. The
	// client polls for that rather than this request hanging: a POST that
	// blocks for a minute is indistinguishable from a broken dashboard, which
	// is the same reason the compose runner streams.
	httpx.JSON(w, http.StatusAccepted, map[string]any{
		"container": name, "engine": req.Engine, "driver": tmpl.driver,
		"host": "127.0.0.1", "port": port, "database": database,
	})
	return nil
}

// freeHostPort returns the engine's own port when nothing holds it, and the
// next free one above it otherwise — so a second Postgres does not fail to
// start with a message about a port collision.
func freeHostPort(preferred int) (int, error) {
	for port := preferred; port < preferred+64 && port < 65536; port++ {
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err == nil {
			ln.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free port near %d to publish this server on", preferred)
}

// generatePassword makes one nobody has to remember. It is URL-safe because it
// goes into a DSN, and long enough that its being reachable only on loopback is
// not the only thing protecting it.
func generatePassword() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
