// Command server runs the VPS management dashboard API.
//
// The process is root-equivalent by design: it drives the Docker socket,
// systemd, the firewall and a PTY. It therefore refuses to start in any
// configuration that would leave it reachable from an untrusted network —
// see internal/config for the allowlist rules.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Wayy01/vps-dashboard/backend/internal/agent"
	"github.com/Wayy01/vps-dashboard/backend/internal/api"
	"github.com/Wayy01/vps-dashboard/backend/internal/audit"
	"github.com/Wayy01/vps-dashboard/backend/internal/auth"
	"github.com/Wayy01/vps-dashboard/backend/internal/config"
	"github.com/Wayy01/vps-dashboard/backend/internal/store"
)

func main() {
	// The container healthcheck re-executes this binary rather than shipping
	// curl into the image, which keeps the runtime surface smaller.
	healthcheck := flag.Bool("healthcheck", false, "probe the local server and exit non-zero if it is unhealthy")
	agentMode := flag.Bool("agent", false, "run as an agent managed by a hub: no human login, mutual TLS only")
	agentReset := flag.Bool("agent-reset", false, "forget the enrolled hub so this agent can be enrolled again, then exit")
	flag.Parse()
	if *healthcheck {
		if err := probe(); err != nil {
			fmt.Fprintln(os.Stderr, "unhealthy:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(*agentMode, *agentReset); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run(agentFlag, agentReset bool) error {
	level := slog.LevelInfo
	if os.Getenv("VPSD_LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// The flag is the ergonomic form; the environment variable is what the
	// compose file sets. Either turns agent mode on.
	cfg.AgentMode = cfg.AgentMode || agentFlag
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	defer st.Close()

	sealer, err := auth.NewSealer(cfg.MasterKeyHex)
	if err != nil {
		return err
	}
	svc := auth.NewService(st, sealer, cfg.SessionTTL, cfg.IdleTTL, cfg.Require2FA)
	aud := audit.New(st, log)

	var identity *agent.Identity
	if cfg.AgentMode {
		identity, err = agent.Load(filepath.Join(cfg.DataDir, "agent"))
		if err != nil {
			return err
		}
		if agentReset {
			if err := identity.Reset(); err != nil {
				return err
			}
			log.Warn("agent enrolment cleared; start the agent again to mint a new token",
				"agent_id", identity.ID())
			return nil
		}
		if err := announceAgent(identity, log); err != nil {
			return err
		}
	} else {
		// An agent has no interactive account to bootstrap: the hub is the
		// only caller, and it authenticates with a certificate.
		if err := bootstrapAdmin(context.Background(), svc, st, log); err != nil {
			return err
		}
	}

	srv := api.New(cfg, log, st, svc, sealer, aud, identity)
	defer srv.Shutdown()

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: log tails, metric pushes and PTY sockets are
		// long-lived by nature and would be severed by one.
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("start background workers: %w", err)
	}
	go janitor(ctx, svc, log)

	if cfg.AgentMode {
		tlsCfg, err := agentTLS(identity)
		if err != nil {
			return err
		}
		httpSrv.TLSConfig = tlsCfg
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("vps-dashboard listening",
			"addr", cfg.Addr, "allowlist", len(cfg.AllowedCIDRs),
			"require2fa", cfg.Require2FA, "agent", cfg.AgentMode)
		var err error
		if cfg.AgentMode {
			// The certificate and key are already in the TLS config.
			err = httpSrv.ListenAndServeTLS("", "")
		} else {
			err = httpSrv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutCtx)
	}
}

// probe asks the local server whether it is serving. It deliberately targets
// the configured bind address so a healthcheck cannot pass against some other
// process that happens to be listening.
func probe() error {
	addr := os.Getenv("VPSD_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	if strings.HasPrefix(addr, "0.0.0.0:") {
		addr = "127.0.0.1:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %s", resp.Status)
	}
	return nil
}

// janitor sweeps expired sessions so a stolen cookie cannot outlive its window
// simply because nobody logged in again.
func janitor(ctx context.Context, svc *auth.Service, log *slog.Logger) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := svc.PurgeExpired(ctx); err != nil {
				log.Warn("session purge failed", "err", err)
			}
		}
	}
}

// bootstrapAdmin creates the first admin on an empty database. The generated
// password is printed once to the process log and must be changed at first
// login; two-factor enrollment is then forced before the account can do
// anything at all.
func bootstrapAdmin(ctx context.Context, svc *auth.Service, st *store.Store, log *slog.Logger) error {
	users, err := svc.ListUsers(ctx)
	if err != nil {
		return err
	}
	if len(users) > 0 {
		return nil
	}
	username := os.Getenv("VPSD_BOOTSTRAP_USER")
	if username == "" {
		username = "admin"
	}
	password := os.Getenv("VPSD_BOOTSTRAP_PASSWORD")
	generated := password == ""
	if generated {
		password = auth.RandomToken(18)
	}
	if _, err := svc.CreateUser(ctx, username, password, auth.RoleAdmin, true); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	if generated {
		log.Warn("bootstrap admin created — change this password immediately",
			"username", username, "password", password)
	} else {
		log.Warn("bootstrap admin created from VPSD_BOOTSTRAP_PASSWORD", "username", username)
	}
	return st.SetSetting(ctx, "bootstrapped_at", time.Now().UTC().Format(time.RFC3339))
}

// announceAgent prints the enrolment token exactly once per boot while the
// agent is unclaimed, in the same spirit as the generated bootstrap password:
// the operator reads it out of the log, carries it to the hub, and it stops
// working the moment it is used or the window closes.
func announceAgent(id *agent.Identity, log *slog.Logger) error {
	if id.Enrolled() {
		log.Info("agent is enrolled",
			"agent_id", id.ID(), "hub_fingerprint", id.HubFingerprint(),
			"enrolled_at", id.EnrolledAt().Format(time.RFC3339))
		return nil
	}
	token, err := id.NewEnrolmentToken()
	if err != nil {
		return err
	}
	log.Warn("agent is not enrolled — add it to a hub with this token",
		"agent_id", id.ID(),
		"fingerprint", id.Fingerprint(),
		"enrolment_token", token,
		"expires_in", agent.TokenTTL.String())
	return nil
}

// agentTLS asks every caller for a certificate without requiring one at the
// handshake. Requiring it here would lock out /agent/enrol, which by
// definition runs before the hub is trusted; the HubOnly middleware does the
// actual enforcement per route, so an unenrolled caller can reach enrolment
// and nothing else.
func agentTLS(id *agent.Identity) (*tls.Config, error) {
	certPEM, keyPEM := id.TLSCertificate()
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("agent tls material: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{pair},
		MinVersion:   tls.VersionTLS12,
		ClientAuth:   tls.RequestClientCert,
	}, nil
}
