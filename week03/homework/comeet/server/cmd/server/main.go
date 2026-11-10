package main

import (
	"comeet/server/internal/config"
	httpapi "comeet/server/internal/http"
	"comeet/server/internal/realtime"
	"comeet/server/internal/store"
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	env := flag.String("env-file", "", "optional dotenv file; existing process variables take priority")
	initEnv := flag.Bool("init-env", false, "initialize a private JWT key without starting the server")
	flag.Parse()
	if *initEnv {
		if err := config.InitializeEnv(*env); err != nil {
			slog.Error("env_initialization_failed", "error", err)
			os.Exit(1)
		}
		slog.Info("local_env_ready", "message", "JWT key retained or generated; value is never printed")
		return
	}
	c, e := config.Load(*env)
	if e != nil {
		slog.Error("configuration_failed", "error", e)
		os.Exit(1)
	}
	db, e := store.Open(c.DatabasePath)
	if e != nil {
		slog.Error("database_initialization_failed")
		os.Exit(1)
	}
	defer db.Close()
	hub := realtime.New(db, c)
	if e = hub.Recover(context.Background()); e != nil {
		slog.Error("recovery_failed")
		os.Exit(1)
	}
	hub.Start()
	defer hub.Shutdown()
	srv := &http.Server{Addr: c.Addr, Handler: httpapi.Router(db, c, hub), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { slog.Info("server_start", "address", c.Addr, "stage", 3); done <- srv.ListenAndServe() }()
	select {
	case e := <-done:
		if e != http.ErrServerClosed {
			slog.Error("server_failed", "error", e)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := srv.Shutdown(shutdown); e != nil {
			srv.Close()
		}
	}
}
