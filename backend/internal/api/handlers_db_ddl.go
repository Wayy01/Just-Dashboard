package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/dbx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
)

// Schema editing and bulk loading, split from handlers_db.go because the data
// surface and the schema surface are different jobs with different guards, and
// one file carrying both had stopped being readable.
//
// The split in capability follows what the query classifier already decided:
// CREATE is medium risk and needs service.control, DROP and TRUNCATE are
// critical and need the destructive capability plus a typed confirmation. A
// form must not be a cheaper way to do what the SQL console gates.

type ddlRequest struct {
	Schema  string          `json:"schema"`
	Table   string          `json:"table"`
	Columns []dbx.NewColumn `json:"columns"`
	Column  dbx.NewColumn   `json:"column"`
	Name    string          `json:"name"`
	Unique  bool            `json:"unique"`
	Fields  []string        `json:"fields"`
	To      string          `json:"to"`
	Kind    string          `json:"kind"`
}

// ddlContext resolves the pool and decodes the request, which every handler
// below needs in the same order.
func (s *Server) ddlContext(r *http.Request) (*ddlRequest, *dbConnection, error) {
	id, err := parseID(r)
	if err != nil {
		return nil, nil, err
	}
	var req ddlRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(req.Table) == "" {
		return nil, nil, httpx.BadRequest("table is required")
	}
	_, conn, err := s.dbPool(r.Context(), id)
	if err != nil {
		return nil, nil, err
	}
	return &req, conn, nil
}

func (s *Server) handleDDLCreateTable(w http.ResponseWriter, r *http.Request) error {
	req, conn, err := s.ddlContext(r)
	if err != nil {
		return err
	}
	pool, _, _ := s.dbPool(r.Context(), conn.ID)
	ctx, cancel := timeoutCtx(r, 60*time.Second)
	defer cancel()
	stmt, err := dbx.CreateTable(ctx, pool, conn.Driver, req.Schema, req.Table, req.Columns)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "database.ddl.create_table", conn.Name,
		map[string]any{"table": req.Table, "statement": stmt})
	httpx.JSON(w, http.StatusOK, map[string]any{"statement": stmt})
	return nil
}

func (s *Server) handleDDLAddColumn(w http.ResponseWriter, r *http.Request) error {
	req, conn, err := s.ddlContext(r)
	if err != nil {
		return err
	}
	pool, _, _ := s.dbPool(r.Context(), conn.ID)
	ctx, cancel := timeoutCtx(r, 60*time.Second)
	defer cancel()
	stmt, err := dbx.AddColumn(ctx, pool, conn.Driver, req.Schema, req.Table, req.Column)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "database.ddl.add_column", conn.Name,
		map[string]any{"table": req.Table, "column": req.Column.Name, "statement": stmt})
	httpx.JSON(w, http.StatusOK, map[string]any{"statement": stmt})
	return nil
}

func (s *Server) handleDDLCreateIndex(w http.ResponseWriter, r *http.Request) error {
	req, conn, err := s.ddlContext(r)
	if err != nil {
		return err
	}
	pool, _, _ := s.dbPool(r.Context(), conn.ID)
	// Building an index locks or rewrites a large table on several engines, so
	// this gets the long timeout the dumps get rather than the short one the
	// other DDL uses.
	ctx, cancel := timeoutCtx(r, 30*time.Minute)
	defer cancel()
	stmt, err := dbx.CreateIndex(ctx, pool, conn.Driver, req.Schema, req.Table, req.Name, req.Fields, req.Unique)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "database.ddl.create_index", conn.Name,
		map[string]any{"table": req.Table, "index": req.Name, "statement": stmt})
	httpx.JSON(w, http.StatusOK, map[string]any{"statement": stmt})
	return nil
}

// handleDDLRename covers both a table rename and a column rename, because they
// are the same intention and differ only in which name is being changed.
func (s *Server) handleDDLRename(w http.ResponseWriter, r *http.Request) error {
	req, conn, err := s.ddlContext(r)
	if err != nil {
		return err
	}
	if strings.TrimSpace(req.To) == "" {
		return httpx.BadRequest("a new name is required")
	}
	pool, _, _ := s.dbPool(r.Context(), conn.ID)
	ctx, cancel := timeoutCtx(r, 60*time.Second)
	defer cancel()

	var stmt string
	if req.Kind == "column" {
		if strings.TrimSpace(req.Name) == "" {
			return httpx.BadRequest("the column to rename is required")
		}
		stmt, err = dbx.RenameColumn(ctx, pool, conn.Driver, req.Schema, req.Table, req.Name, req.To)
	} else {
		stmt, err = dbx.RenameTable(ctx, pool, conn.Driver, req.Schema, req.Table, req.To)
	}
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "database.ddl.rename", conn.Name,
		map[string]any{"kind": req.Kind, "table": req.Table, "to": req.To, "statement": stmt})
	httpx.JSON(w, http.StatusOK, map[string]any{"statement": stmt})
	return nil
}

// --- destructive schema changes -------------------------------------------

func (s *Server) handleDDLDropTable(w http.ResponseWriter, r *http.Request) error {
	req, conn, err := s.ddlContext(r)
	if err != nil {
		return err
	}
	// Confirming on the table name forces the operator to read which table they
	// are destroying, exactly as DROP through the query runner does.
	if err := httpx.RequireTypedConfirmation(w, r, req.Table); err != nil {
		return err
	}
	pool, _, _ := s.dbPool(r.Context(), conn.ID)
	ctx, cancel := timeoutCtx(r, 5*time.Minute)
	defer cancel()
	stmt, err := dbx.DropTable(ctx, pool, conn.Driver, req.Schema, req.Table)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "database.ddl.drop_table", conn.Name,
		map[string]any{"table": req.Table, "statement": stmt})
	httpx.JSON(w, http.StatusOK, map[string]any{"statement": stmt})
	return nil
}

func (s *Server) handleDDLDropColumn(w http.ResponseWriter, r *http.Request) error {
	req, conn, err := s.ddlContext(r)
	if err != nil {
		return err
	}
	if strings.TrimSpace(req.Name) == "" {
		return httpx.BadRequest("the column to drop is required")
	}
	// The column name, not the table's: this destroys one column's data and the
	// phrase should name what is actually being lost.
	if err := httpx.RequireTypedConfirmation(w, r, req.Name); err != nil {
		return err
	}
	pool, _, _ := s.dbPool(r.Context(), conn.ID)
	ctx, cancel := timeoutCtx(r, 5*time.Minute)
	defer cancel()
	stmt, err := dbx.DropColumn(ctx, pool, conn.Driver, req.Schema, req.Table, req.Name)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "database.ddl.drop_column", conn.Name,
		map[string]any{"table": req.Table, "column": req.Name, "statement": stmt})
	httpx.JSON(w, http.StatusOK, map[string]any{"statement": stmt})
	return nil
}

func (s *Server) handleDDLDropIndex(w http.ResponseWriter, r *http.Request) error {
	req, conn, err := s.ddlContext(r)
	if err != nil {
		return err
	}
	if strings.TrimSpace(req.Name) == "" {
		return httpx.BadRequest("the index to drop is required")
	}
	if err := httpx.RequireTypedConfirmation(w, r, req.Name); err != nil {
		return err
	}
	pool, _, _ := s.dbPool(r.Context(), conn.ID)
	ctx, cancel := timeoutCtx(r, 5*time.Minute)
	defer cancel()
	stmt, err := dbx.DropIndex(ctx, pool, conn.Driver, req.Schema, req.Table, req.Name)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "database.ddl.drop_index", conn.Name,
		map[string]any{"table": req.Table, "index": req.Name, "statement": stmt})
	httpx.JSON(w, http.StatusOK, map[string]any{"statement": stmt})
	return nil
}

func (s *Server) handleDDLTruncate(w http.ResponseWriter, r *http.Request) error {
	req, conn, err := s.ddlContext(r)
	if err != nil {
		return err
	}
	if err := httpx.RequireTypedConfirmation(w, r, req.Table); err != nil {
		return err
	}
	pool, _, _ := s.dbPool(r.Context(), conn.ID)
	ctx, cancel := timeoutCtx(r, 5*time.Minute)
	defer cancel()
	stmt, err := dbx.TruncateTable(ctx, pool, conn.Driver, req.Schema, req.Table)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "database.ddl.truncate", conn.Name,
		map[string]any{"table": req.Table, "statement": stmt})
	httpx.JSON(w, http.StatusOK, map[string]any{"statement": stmt})
	return nil
}

// --- import ---------------------------------------------------------------

type importRequest struct {
	Schema      string   `json:"schema"`
	Table       string   `json:"table"`
	Format      string   `json:"format"`
	Data        string   `json:"data"`
	Columns     []string `json:"columns"`
	HasHeader   bool     `json:"hasHeader"`
	Truncate    bool     `json:"truncate"`
	StopOnError bool     `json:"stopOnError"`
	NullAs      string   `json:"nullAs"`
}

// handleDBImport loads pasted or uploaded data into a table.
//
// The body carries the data inline rather than as a multipart upload, which
// bounds it at DecodeJSON's 4 MB cap. That is a deliberate ceiling: a load
// larger than that belongs in the engine's own bulk loader, which is faster by
// orders of magnitude and does not hold an HTTP request open for it.
//
// Truncate makes this destructive, so it demands the capability and a typed
// confirmation by hand — the route cannot know, exactly as the query runner
// cannot know from its path whether the SQL in it deletes anything.
func (s *Server) handleDBImport(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	var req importRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Table) == "" {
		return httpx.BadRequest("table is required")
	}
	if strings.TrimSpace(req.Data) == "" {
		return httpx.BadRequest("no data to import")
	}
	if req.Truncate {
		p := httpx.MustPrincipal(r)
		if !p.Can(auth.CapDestructive) {
			return httpx.Err(http.StatusForbidden, "forbidden",
				"replacing a table's contents is destructive and your role does not permit it")
		}
		if err := httpx.RequireTypedConfirmation(w, r, req.Table); err != nil {
			return err
		}
		if !s.destrLim.Allow(p.Username() + "|dbimport") {
			return httpx.Err(http.StatusTooManyRequests, "rate_limited",
				"too many destructive imports, slow down")
		}
	}
	pool, conn, err := s.dbPool(r.Context(), id)
	if err != nil {
		return err
	}
	opts := dbx.ImportOptions{
		Schema: req.Schema, Table: req.Table, Columns: req.Columns,
		HasHeader: req.HasHeader, Truncate: req.Truncate,
		StopOnError: req.StopOnError, NullAs: req.NullAs,
	}
	ctx, cancel := timeoutCtx(r, 15*time.Minute)
	defer cancel()

	var res *dbx.ImportResult
	if strings.EqualFold(req.Format, "json") {
		res, err = dbx.ImportJSON(ctx, pool, conn.Driver, strings.NewReader(req.Data), opts)
	} else {
		res, err = dbx.ImportCSV(ctx, pool, conn.Driver, strings.NewReader(req.Data), opts)
	}
	if err != nil {
		httpx.SetAudit(r, "database.import", conn.Name,
			map[string]any{"table": req.Table, "error": err.Error()})
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "database.import", conn.Name, map[string]any{
		"table": req.Table, "inserted": res.Inserted, "failed": res.Failed,
		"truncated": req.Truncate,
	})
	httpx.JSON(w, http.StatusOK, res)
	return nil
}
