// Package main provides the HTTP entrypoint for scheduled background jobs
// (event lifecycle transitions and expired notification cleanup) deployed on Cloud Run.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/kaanunsel/social-event-mapper/backend/internal/bootstrap"
	"github.com/kaanunsel/social-event-mapper/backend/internal/infrastructure/observability"
)

const (
	lifecycleLockID int64 = 48001
	cleanupLockID   int64 = 48002
)

func main() {
	ctx := context.Background()
	observability.ConfigureLogger(nil)

	container, err := bootstrap.New(ctx)
	if err != nil {
		slog.Error("worker bootstrap failed", "error", err)
		os.Exit(1)
	}
	defer container.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("POST /lifecycle", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		acquired, unlock, err := tryAdvisoryLock(r.Context(), container, lifecycleLockID)
		if err != nil {
			slog.ErrorContext(r.Context(), "lifecycle advisory lock failed", "error", err)
			http.Error(w, "lock error", http.StatusInternalServerError)
			return
		}
		if !acquired {
			writeJSON(w, http.StatusOK, map[string]any{"status": "SKIPPED_OVERLAP"})
			return
		}
		defer unlock()

		if err := container.EventService.TransitionEventStatuses(r.Context()); err != nil {
			slog.ErrorContext(r.Context(), "lifecycle transition failed", "error", err)
			http.Error(w, "transition failed", http.StatusInternalServerError)
			return
		}

		slog.InfoContext(r.Context(), "lifecycle transition completed",
			"operation", "worker.lifecycle",
			"duration_ms", time.Since(start).Milliseconds(),
		)
		writeJSON(w, http.StatusOK, map[string]any{"status": "SUCCEEDED"})
	})

	mux.HandleFunc("POST /cleanup", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		acquired, unlock, err := tryAdvisoryLock(r.Context(), container, cleanupLockID)
		if err != nil {
			slog.ErrorContext(r.Context(), "cleanup advisory lock failed", "error", err)
			http.Error(w, "lock error", http.StatusInternalServerError)
			return
		}
		if !acquired {
			writeJSON(w, http.StatusOK, map[string]any{"status": "SKIPPED_OVERLAP"})
			return
		}
		defer unlock()

		deleted, err := container.NotificationService.DeleteExpiredNotifications(r.Context())
		if err != nil {
			slog.ErrorContext(r.Context(), "notification cleanup failed", "error", err)
			http.Error(w, "cleanup failed", http.StatusInternalServerError)
			return
		}

		slog.InfoContext(r.Context(), "notification cleanup completed",
			"operation", "worker.cleanup",
			"deleted_count", deleted,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		writeJSON(w, http.StatusOK, map[string]any{"status": "SUCCEEDED", "deleted_count": deleted})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("starting worker server", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("worker server stopped", "error", err)
		os.Exit(1)
	}
}

func tryAdvisoryLock(ctx context.Context, c *bootstrap.Container, lockID int64) (bool, func(), error) {
	conn, err := c.DB.Acquire(ctx)
	if err != nil {
		return false, nil, err
	}

	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lockID).Scan(&acquired); err != nil {
		conn.Release()
		return false, nil, err
	}
	if !acquired {
		conn.Release()
		return false, nil, nil
	}

	unlock := func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", lockID)
		conn.Release()
	}
	return true, unlock, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
