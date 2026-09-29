// Package api is the public HTTP API. Mode-specific routes live in
// subpackages (api/decision today; later api/image, api/tts, ...).
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"ai-server/internal/errs"
)

// ModelInfo is the typed body of /v1/model. Capabilities is mode-specific.
type ModelInfo struct {
	ID           string `json:"id"`
	Object       string `json:"object"`
	Type         string `json:"type"`
	Quant        string `json:"quant"`
	Capabilities any    `json:"capabilities"`
	// Info is registry metadata (family, parameters, license, context, ...).
	Info any `json:"info,omitempty"`
	// Settings are the effective engine settings.
	Settings map[string]any `json:"settings,omitempty"`
}

// Health reports runner state.
type Health interface {
	// RunnerState returns "ready", "crashed" or "stopping".
	RunnerState() string
}

// Mount registers one model type's routes and returns its metadata.
type Mount func(r chi.Router) ModelInfo

// NewRouter builds the router for a single loaded model.
func NewRouter(mount Mount, health Health, log *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(recoverer(log))
	r.Use(requestLog(log))
	info := mount(r)

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		state := health.RunnerState()
		status, code := "ok", http.StatusOK
		if state != "ready" {
			status, code = "unavailable", http.StatusServiceUnavailable
		}
		WriteJSON(w, code, map[string]string{"status": status, "model": info.ID, "runner": state})
	})

	r.Get("/v1/model", func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, info)
	})

	r.Get("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, struct {
			Object string      `json:"object"`
			Data   []ModelInfo `json:"data"`
		}{"list", []ModelInfo{info}})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, errs.New(errs.InvalidRequest, "no route for %s %s", r.Method, r.URL.Path))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET, POST")
		WriteJSON(w, http.StatusMethodNotAllowed, errorBody{errorDetail{errs.InvalidRequest, "method not allowed"}})
	})
	return r
}

func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v)
					}
					log.Error("panic in handler", "path", r.URL.Path, "panic", v)
					WriteError(w, errs.New(errs.Internal, ""))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func requestLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			log.Debug("request", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "ms", time.Since(start).Milliseconds())
		})
	}
}
