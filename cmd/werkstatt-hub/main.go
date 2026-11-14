// Werkstatt-Hub is an offline-first server for the local workshop network.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"werkstatt-hub/internal/config"
	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/storage"
	"werkstatt-hub/internal/web"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out, errOut io.Writer) error {
	command := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}
	if command != "serve" && command != "init" && command != "migrate" {
		return fmt.Errorf("Befehl muss serve, init oder migrate sein")
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(errOut)
	path := flags.String("config", "", "Lokale JSON-Konfiguration (optional)")
	var admin *string
	if command == "init" {
		admin = flags.String("admin", "", "Name des ersten Admins")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unerwartete Argumente")
	}
	if admin != nil && *admin == "" {
		return fmt.Errorf("init benötigt --admin mit einem Namen")
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	store, err := storage.OpenWithOptions(cfg.Database, storage.Options{PriorityScale: core.PriorityScale{Min: cfg.PriorityMin, Max: cfg.PriorityMax}})
	if err != nil {
		return fmt.Errorf("Datenbank öffnen: %w", err)
	}
	defer store.Close()
	switch command {
	case "init":
		u, err := store.BootstrapAdmin(ctx, *admin)
		if err != nil {
			return fmt.Errorf("Admin einrichten: %w", err)
		}
		_, err = fmt.Fprintf(out, "Admin eingerichtet (ID %d). App mit serve starten; weitere Nutzer unter /admin/users anlegen.\n", u.ID)
		return err
	case "migrate":
		_, err = fmt.Fprintln(out, "Alle versionierten Migrationen sind angewendet.")
		return err
	}
	ttl, _ := time.ParseDuration(cfg.SessionTTL)
	handler, err := web.New(store, web.Options{SecureCookies: cfg.SecureCookies, SessionTTL: ttl})
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("HTTP starten: %w", err)
	}
	fmt.Fprintf(out, "Werkstatt-Hub auf %s (HTTP); Datenbank: %s\n", listener.Addr(), cfg.Database)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		err := <-done
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
