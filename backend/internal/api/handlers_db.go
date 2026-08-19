package api

import (
	"context"
	"database/sql"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/dbx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/go-chi/chi/v5"
)

// dbConnection is the client-facing view. The DSN is never included: it holds
// credentials, and the dashboard has no reason to hand them back out.
type dbConnection struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Driver    dbx.Driver `json:"driver"`
	Host      string     `json:"host"`
	Port      string     `json:"port"`
	User      string     `json:"user"`
	Database  string     `json:"database"`
	CreatedAt time.Time  `json:"createdAt"`
}

func (s *Server) mountDatabaseRoutes(r chi.Router) {
	r.Route("/databases", func(r chi.Router) {
		r.Method(http.MethodGet, "/", s.handle(s.handleDBConnList))
		r.Group(func(r chi.Router) {
			r.Use(httpx.RequireCapability(auth.CapSystemAdmin))
			r.Method(http.MethodPost, "/", s.handle(s.handleDBConnCreate))
			r.Method(http.MethodDelete, "/{id}", s.handle(s.handleDBConnDelete))
		})
		r.Method(http.MethodGet, "/{id}/ping", s.handle(s.handleDBPing))
		r.Method(http.MethodGet, "/{id}/stats", s.handle(s.handleDBStats))
		r.Method(http.MethodGet, "/{id}/schemas", s.handle(s.handleDBList))
		r.Method(http.MethodGet, "/{id}/tables", s.handle(s.handleDBTables))
		r.Method(http.MethodGet, "/{id}/columns", s.handle(s.handleDBColumns))
		r.Method(http.MethodGet, "/{id}/browse", s.handle(s.handleDBBrowse))
		r.Method(http.MethodPost, "/{id}/classify", s.handle(s.handleDBClassify))
		r.Group(func(r chi.Router) {
			r.Use(httpx.RequireCapability(auth.CapServiceControl))
			r.Method(http.MethodPost, "/{id}/query", s.handle(s.handleDBQuery))
			r.Method(http.MethodPost, "/{id}/backup", s.handle(s.handleDBBackup))
		})
		s.destructive(r, func(r chi.Router) {
			r.Method(http.MethodPost, "/{id}/restore", s.handle(s.handleDBRestore))
		})
	})
}

func (s *Server) dbConnRow(ctx context.Context, id int64) (*dbConnection, string, error) {
	var (
		name, driver, dsnEnc string
		created              int64
	)
	err := s.Store.DB.QueryRowContext(ctx,
		`SELECT name, driver, dsn_enc, created_at FROM db_connections WHERE id = ?`, id).
		Scan(&name, &driver, &dsnEnc, &created)
	if err == sql.ErrNoRows {
		return nil, "", httpx.ErrNotFound
	}
	if err != nil {
		return nil, "", httpx.Internal(err)
	}
	dsn, err := s.Sealer.Open(dsnEnc)
	if err != nil {
		return nil, "", httpx.Internal(err)
	}
	conn := &dbConnection{
		ID: id, Name: name, Driver: dbx.Driver(driver),
		CreatedAt: time.Unix(created, 0).UTC(),
	}
	if info, err := dbx.ParseDSN(conn.Driver, dsn); err == nil {
		conn.Host, conn.Port, conn.User, conn.Database = info.Host, info.Port, info.User, info.Database
	}
	return conn, dsn, nil
}

func (s *Server) dbPool(ctx context.Context, id int64) (*sql.DB, *dbConnection, error) {
	conn, dsn, err := s.dbConnRow(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if conn.Driver == dbx.DriverMongo {
		return nil, conn, httpx.BadRequest("this endpoint is not available for MongoDB connections")
	}
	pool, err := s.modules.dbs.Pool(ctx, id, conn.Driver, dsn)
	if err != nil {
		return nil, conn, httpx.Err(http.StatusBadGateway, "connect_failed", err.Error())
	}
	return pool, conn, nil
}

func (s *Server) handleDBConnList(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.Store.DB.QueryContext(r.Context(),
		`SELECT id FROM db_connections ORDER BY name`)
	if err != nil {
		return httpx.Internal(err)
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return httpx.Internal(err)
		}
		ids = append(ids, id)
	}
	out := []*dbConnection{}
	for _, id := range ids {
		conn, _, err := s.dbConnRow(r.Context(), id)
		if err != nil {
			continue
		}
		out = append(out, conn)
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

type createDBConnRequest struct {
	Name   string     `json:"name"`
	Driver dbx.Driver `json:"driver"`
	DSN    string     `json:"dsn"`
}

func (s *Server) handleDBConnCreate(w http.ResponseWriter, r *http.Request) error {
	var req createDBConnRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if !req.Driver.Valid() {
		return httpx.BadRequest("driver must be one of postgres, mysql, mongodb")
	}
	if req.Name == "" || req.DSN == "" {
		return httpx.BadRequest("name and dsn are required")
	}
	sealed, err := s.Sealer.Seal(req.DSN)
	if err != nil {
		return httpx.Internal(err)
	}
	res, err := s.Store.DB.ExecContext(r.Context(),
		`INSERT INTO db_connections(name, driver, dsn_enc, created_at) VALUES(?,?,?,?)`,
		req.Name, string(req.Driver), sealed, time.Now().Unix())
	if err != nil {
		return httpx.BadRequest("could not save connection: %v", err)
	}
	id, _ := res.LastInsertId()
	conn, _, err := s.dbConnRow(r.Context(), id)
	if err != nil {
		return err
	}
	// The DSN itself never reaches the audit trail; the parsed host and user
	// identify the connection without leaking the password.
	httpx.SetAudit(r, "database.connection.create", req.Name,
		map[string]any{"driver": req.Driver, "host": conn.Host, "user": conn.User})
	httpx.JSON(w, http.StatusCreated, conn)
	return nil
}

func (s *Server) handleDBConnDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		return httpx.BadRequest("invalid id")
	}
	conn, _, err := s.dbConnRow(r.Context(), id)
	if err != nil {
		return err
	}
	if err := httpx.RequireTypedConfirmation(w, r, conn.Name); err != nil {
		return err
	}
	s.modules.dbs.Close(id)
	if _, err := s.Store.DB.ExecContext(r.Context(), `DELETE FROM db_connections WHERE id = ?`, id); err != nil {
		return httpx.Internal(err)
	}
	httpx.SetAudit(r, "database.connection.delete", conn.Name, nil)
	httpx.NoContent(w)
	return nil
}

func parseID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		return 0, httpx.BadRequest("invalid id")
	}
	return id, nil
}

func (s *Server) handleDBPing(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	conn, dsn, err := s.dbConnRow(r.Context(), id)
	if err != nil {
		return err
	}
	if conn.Driver == dbx.DriverMongo {
		client, err := dbx.MongoClient(r.Context(), dsn)
		if err != nil {
			httpx.JSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return nil
		}
		defer client.Disconnect(context.Background())
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
		return nil
	}
	if _, _, err := s.dbPool(r.Context(), id); err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return nil
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

func (s *Server) handleDBStats(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	conn, dsn, err := s.dbConnRow(r.Context(), id)
	if err != nil {
		return err
	}
	if conn.Driver == dbx.DriverMongo {
		client, err := dbx.MongoClient(r.Context(), dsn)
		if err != nil {
			return httpx.Err(http.StatusBadGateway, "connect_failed", err.Error())
		}
		defer client.Disconnect(context.Background())
		status, err := dbx.MongoServerStatus(r.Context(), client)
		if err != nil {
			return httpx.Err(http.StatusBadGateway, "query_failed", err.Error())
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"server": status})
		return nil
	}
	if _, _, err := s.dbPool(r.Context(), id); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"pool": s.modules.dbs.Stats(id)})
	return nil
}

func (s *Server) handleDBList(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	conn, dsn, err := s.dbConnRow(r.Context(), id)
	if err != nil {
		return err
	}
	if conn.Driver == dbx.DriverMongo {
		client, err := dbx.MongoClient(r.Context(), dsn)
		if err != nil {
			return httpx.Err(http.StatusBadGateway, "connect_failed", err.Error())
		}
		defer client.Disconnect(context.Background())
		dbs, err := dbx.MongoDatabases(r.Context(), client)
		if err != nil {
			return httpx.Err(http.StatusBadGateway, "query_failed", err.Error())
		}
		httpx.JSON(w, http.StatusOK, dbs)
		return nil
	}
	pool, conn, err := s.dbPool(r.Context(), id)
	if err != nil {
		return err
	}
	dbs, err := dbx.ListDatabases(r.Context(), pool, conn.Driver)
	if err != nil {
		return httpx.Err(http.StatusBadGateway, "query_failed", err.Error())
	}
	httpx.JSON(w, http.StatusOK, dbs)
	return nil
}

func (s *Server) handleDBTables(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	conn, dsn, err := s.dbConnRow(r.Context(), id)
	if err != nil {
		return err
	}
	schema := r.URL.Query().Get("schema")
	if conn.Driver == dbx.DriverMongo {
		client, err := dbx.MongoClient(r.Context(), dsn)
		if err != nil {
			return httpx.Err(http.StatusBadGateway, "connect_failed", err.Error())
		}
		defer client.Disconnect(context.Background())
		if schema == "" {
			schema = conn.Database
		}
		if schema == "" {
			return httpx.BadRequest("schema query parameter is required for MongoDB")
		}
		cols, err := dbx.MongoCollections(r.Context(), client, schema)
		if err != nil {
			return httpx.Err(http.StatusBadGateway, "query_failed", err.Error())
		}
		httpx.JSON(w, http.StatusOK, cols)
		return nil
	}
	pool, conn, err := s.dbPool(r.Context(), id)
	if err != nil {
		return err
	}
	tables, err := dbx.ListTables(r.Context(), pool, conn.Driver, schema)
	if err != nil {
		return httpx.Err(http.StatusBadGateway, "query_failed", err.Error())
	}
	httpx.JSON(w, http.StatusOK, tables)
	return nil
}

func (s *Server) handleDBColumns(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	pool, conn, err := s.dbPool(r.Context(), id)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	cols, err := dbx.ListColumns(r.Context(), pool, conn.Driver, q.Get("schema"), q.Get("table"))
	if err != nil {
		return httpx.Err(http.StatusBadGateway, "query_failed", err.Error())
	}
	httpx.JSON(w, http.StatusOK, cols)
	return nil
}

func (s *Server) handleDBBrowse(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	conn, dsn, err := s.dbConnRow(r.Context(), id)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	limit := atoiDefault(q.Get("limit"), 100)
	offset := atoiDefault(q.Get("offset"), 0)

	if conn.Driver == dbx.DriverMongo {
		client, err := dbx.MongoClient(r.Context(), dsn)
		if err != nil {
			return httpx.Err(http.StatusBadGateway, "connect_failed", err.Error())
		}
		defer client.Disconnect(context.Background())
		res, err := dbx.MongoFind(r.Context(), client, q.Get("schema"), q.Get("table"), q.Get("filter"), limit, offset)
		if err != nil {
			return httpx.BadRequest("%v", err)
		}
		httpx.JSON(w, http.StatusOK, res)
		return nil
	}
	pool, conn, err := s.dbPool(r.Context(), id)
	if err != nil {
		return err
	}
	ctx, cancel := timeoutCtx(r, 60*time.Second)
	defer cancel()
	res, err := dbx.BrowseTable(ctx, pool, conn.Driver, q.Get("schema"), q.Get("table"), limit, offset)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

type queryRequest struct {
	Query   string `json:"query"`
	MaxRows int    `json:"maxRows"`
}

// handleDBClassify lets the editor warn before anything is sent. It is a pure
// analysis of the text and touches no database.
func (s *Server) handleDBClassify(w http.ResponseWriter, r *http.Request) error {
	var req queryRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	httpx.SkipAudit(r)
	httpx.JSON(w, http.StatusOK, dbx.Classify(req.Query))
	return nil
}

// handleDBQuery runs arbitrary SQL. A destructive statement additionally
// requires the destructive capability and a typed confirmation, so a
// mistyped DELETE cannot execute on a single click.
func (s *Server) handleDBQuery(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	var req queryRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Query == "" {
		return httpx.BadRequest("query is required")
	}
	risk := dbx.Classify(req.Query)
	p := httpx.MustPrincipal(r)
	if risk.Destructive {
		if !p.Can(auth.CapDestructive) {
			return httpx.Err(http.StatusForbidden, "forbidden",
				"this statement is destructive and your role does not permit it")
		}
		if err := httpx.RequireTypedConfirmation(w, r, "run "+risk.Level); err != nil {
			return err
		}
		if !s.destrLim.Allow(p.Username() + "|dbquery") {
			return httpx.Err(http.StatusTooManyRequests, "rate_limited",
				"too many destructive statements, slow down")
		}
	}
	pool, _, err := s.dbPool(r.Context(), id)
	if err != nil {
		return err
	}
	ctx, cancel := timeoutCtx(r, 120*time.Second)
	defer cancel()
	res, err := dbx.RunQuery(ctx, pool, req.Query, req.MaxRows)
	if err != nil {
		httpx.SetAudit(r, "database.query", strconv.FormatInt(id, 10),
			map[string]any{"risk": risk.Level, "error": err.Error()})
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "database.query", strconv.FormatInt(id, 10), map[string]any{
		"risk": risk.Level, "destructive": risk.Destructive,
		"rowsAffected": res.Affected, "rowCount": res.RowCount, "statement": req.Query,
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"result": res, "risk": risk})
	return nil
}

type dbBackupRequest struct {
	Database string `json:"database"`
}

func (s *Server) handleDBBackup(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	var req dbBackupRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	conn, dsn, err := s.dbConnRow(r.Context(), id)
	if err != nil {
		return err
	}
	ctx, cancel := timeoutCtx(r, 30*time.Minute)
	defer cancel()
	outDir := filepath.Join(s.Cfg.BackupLocalDir, "databases", conn.Name)
	res, err := dbx.Dump(ctx, conn.Driver, dsn, req.Database, outDir)
	if err != nil {
		return httpx.Err(http.StatusBadGateway, "dump_failed", err.Error())
	}
	httpx.SetAudit(r, "database.backup", conn.Name,
		map[string]any{"database": res.Database, "path": res.Path, "size": res.Size})
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

type dbRestoreRequest struct {
	Database string `json:"database"`
	DumpPath string `json:"dumpPath"`
}

func (s *Server) handleDBRestore(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	var req dbRestoreRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	conn, dsn, err := s.dbConnRow(r.Context(), id)
	if err != nil {
		return err
	}
	target := req.Database
	if target == "" {
		target = conn.Database
	}
	// A restore overwrites live data. Confirming on the target database name
	// forces the operator to read which database they are about to replace.
	if err := httpx.RequireTypedConfirmation(w, r, target); err != nil {
		return err
	}
	ctx, cancel := timeoutCtx(r, 60*time.Minute)
	defer cancel()
	out, err := dbx.Restore(ctx, conn.Driver, dsn, req.Database, req.DumpPath)
	if err != nil {
		httpx.SetAudit(r, "database.restore", conn.Name,
			map[string]any{"database": target, "dumpPath": req.DumpPath, "error": err.Error()})
		return httpx.Err(http.StatusBadGateway, "restore_failed", err.Error())
	}
	httpx.SetAudit(r, "database.restore", conn.Name,
		map[string]any{"database": target, "dumpPath": req.DumpPath})
	httpx.JSON(w, http.StatusOK, map[string]string{"output": out})
	return nil
}
