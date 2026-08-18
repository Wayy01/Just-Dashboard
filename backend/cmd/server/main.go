// Command server runs the VPS management dashboard API.
//
// The process is root-equivalent by design: it drives the Docker socket,
// systemd, the firewall and a PTY. It therefore refuses to start in any
// configuration that would leave it reachable from an untrusted network —
// see internal/config for the allowlist rules.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Wayy01/vps-dashboard/backend/internal/api"
	"github.com/Wayy01/vps-dashboard/backend/internal/audit"
	"github.com/Wayy01/vps-dashboard/backend/internal/auth"
	"github.com/Wayy01/vps-dashboard/backend/internal/config"
	"github.com/Wayy01/vps-dashboard/backend/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
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

	if err := bootstrapAdmin(context.Background(), svc, st, log); err != nil {
		return err
	}

	srv := api.New(cfg, log, st, svc, sealer, aud)
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

	errCh := make(chan error, 1)
	go func() {
		log.Info("vps-dashboard listening",
			"addr", cfg.Addr, "allowlist", len(cfg.AllowedCIDRs), "require2fa", cfg.Require2FA)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
