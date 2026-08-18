package api

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wayy01/vps-dashboard/backend/internal/httpx"
	"github.com/Wayy01/vps-dashboard/backend/internal/proxysvc"
	"github.com/go-chi/chi/v5"
)

type watchedDomain struct {
	ID        int64                 `json:"id"`
	Domain    string                `json:"domain"`
	Port      int                   `json:"port"`
	CreatedAt time.Time             `json:"createdAt"`
	Cert      *proxysvc.Certificate `json:"certificate,omitempty"`
}

// handleWatchedDomains checks every watched domain's live certificate. The
// checks run concurrently because each is a TLS handshake against a remote
// host, and doing twenty of them in sequence would make the page feel broken.
func (s *Server) handleWatchedDomains(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.Store.DB.QueryContext(r.Context(),
		`SELECT id, domain, port, created_at FROM watched_domains ORDER BY domain`)
	if err != nil {
		return httpx.Internal(err)
	}
	defer rows.Close()

	domains := []*watchedDomain{}
	for rows.Next() {
		var d watchedDomain
		var created int64
		if err := rows.Scan(&d.ID, &d.Domain, &d.Port, &created); err != nil {
			return httpx.Internal(err)
		}
		d.CreatedAt = time.Unix(created, 0).UTC()
		domains = append(domains, &d)
	}
	if err := rows.Err(); err != nil {
		return httpx.Internal(err)
	}

	if r.URL.Query().Get("check") != "false" {
		ctx, cancel := timeoutCtx(r, 30*time.Second)
		defer cancel()
		var wg sync.WaitGroup
		sem := make(chan struct{}, 8)
		for _, d := range domains {
			wg.Add(1)
			go func(d *watchedDomain) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if cert, err := proxysvc.CheckDomain(ctx, d.Domain, d.Port); err == nil {
					d.Cert = cert
				}
			}(d)
		}
		wg.Wait()
	}
	httpx.JSON(w, http.StatusOK, domains)
	return nil
}

type watchDomainRequest struct {
	Domain string `json:"domain"`
	Port   int    `json:"port"`
}

func (s *Server) handleWatchDomain(w http.ResponseWriter, r *http.Request) error {
	var req watchDomainRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if domain == "" || strings.ContainsAny(domain, "/ \t") {
		return httpx.BadRequest("domain must be a bare hostname, for example example.com")
	}
	if req.Port == 0 {
		req.Port = 443
	}
	res, err := s.Store.DB.ExecContext(r.Context(),
		`INSERT INTO watched_domains(domain, port, created_at) VALUES(?,?,?)
		 ON CONFLICT(domain) DO UPDATE SET port = excluded.port`,
		domain, req.Port, time.Now().Unix())
	if err != nil {
		return httpx.Internal(err)
	}
	id, _ := res.LastInsertId()
	httpx.SetAudit(r, "certificates.watch.add", domain, map[string]any{"port": req.Port})
	httpx.JSON(w, http.StatusCreated, watchedDomain{
		ID: id, Domain: domain, Port: req.Port, CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (s *Server) handleUnwatchDomain(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		return httpx.BadRequest("invalid id")
	}
	var domain string
	if err := s.Store.DB.QueryRowContext(r.Context(),
		`SELECT domain FROM watched_domains WHERE id = ?`, id).Scan(&domain); err != nil {
		return httpx.ErrNotFound
	}
	if _, err := s.Store.DB.ExecContext(r.Context(), `DELETE FROM watched_domains WHERE id = ?`, id); err != nil {
		return httpx.Internal(err)
	}
	httpx.SetAudit(r, "certificates.watch.remove", domain, nil)
	httpx.NoContent(w)
	return nil
}
