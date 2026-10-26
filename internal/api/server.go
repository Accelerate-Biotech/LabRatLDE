// Package api is LabRat's HTTP surface. Everything the UI can do, the API can
// do first: the frontend is just another client.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/config"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/domain"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/ingest"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/storage"
)

const maxJSONBody = 1 << 20

type Server struct {
	cfg config.Config
	db  *storage.DB
	log *slog.Logger
}

func New(cfg config.Config, db *storage.DB, log *slog.Logger) *Server {
	return &Server{cfg: cfg, db: db, log: log}
}

// Handler returns the fully wired router.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)

	v1 := http.NewServeMux()
	v1.HandleFunc("POST /v1/experiments", s.createExperiment)
	v1.HandleFunc("GET /v1/experiments", s.listExperiments)
	v1.HandleFunc("GET /v1/experiments/{id}", s.getExperiment)
	v1.HandleFunc("POST /v1/experiments/{id}/protocol-versions", s.createProtocolVersion)
	v1.HandleFunc("GET /v1/experiments/{id}/protocol-versions", s.listProtocolVersions)
	v1.HandleFunc("GET /v1/protocol-versions/{id}", s.getProtocolVersion)

	v1.HandleFunc("POST /v1/runs", s.createRun)
	v1.HandleFunc("GET /v1/experiments/{id}/runs", s.listExperimentRuns)
	v1.HandleFunc("GET /v1/runs/{id}", s.getRun)
	v1.HandleFunc("POST /v1/runs/{id}/transitions", s.transitionRun)

	v1.HandleFunc("POST /v1/lineage/nodes", s.createNode)
	v1.HandleFunc("POST /v1/lineage/edges", s.createEdge)
	v1.HandleFunc("GET /v1/lineage/{id}", s.getLineage)

	v1.HandleFunc("GET /v1/instruments", s.listInstruments)
	v1.HandleFunc("POST /v1/instruments", s.createInstrument)
	v1.HandleFunc("POST /v1/instruments/{id}/stream", s.streamReadings)

	v1.HandleFunc("GET /v1/audit/events", s.listAuditEvents)
	v1.HandleFunc("GET /v1/audit/export.ndjson", s.exportAudit)
	v1.HandleFunc("GET /v1/audit/verify", s.verifyAudit)

	mux.Handle("/v1/", s.identify(v1))
	return s.logRequests(mux)
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Pool.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "database": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// Identity
// ---------------------------------------------------------------------------

// Principal is the authenticated caller. Every audit event records Actor.
type Principal struct {
	Tenant string
	Actor  string
}

type principalKey struct{}

func principal(ctx context.Context) Principal { return ctx.Value(principalKey{}).(Principal) }

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validUUID(s string) bool { return uuidRE.MatchString(s) }

// identify resolves the caller.
//
// SCAFFOLD ONLY: identity is taken from X-LDE-Tenant / X-LDE-Actor headers,
// which is acceptable for local development and nothing else. Part 11
// (§11.10(d), §11.300) requires authenticated, unique user identity: replace
// this with OIDC bearer-token validation before any regulated use. The
// server refuses to trust these headers when LDE_ENV=prod.
func (s *Server) identify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Env == "prod" {
			writeError(w, http.StatusNotImplemented, "authentication provider not configured (OIDC integration pending)")
			return
		}
		p := Principal{
			Tenant: strings.ToLower(r.Header.Get("X-LDE-Tenant")),
			Actor:  strings.TrimSpace(r.Header.Get("X-LDE-Actor")),
		}
		if !validUUID(p.Tenant) || p.Actor == "" {
			writeError(w, http.StatusUnauthorized, "X-LDE-Tenant (uuid) and X-LDE-Actor headers are required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

// ---------------------------------------------------------------------------
// Plumbing
// ---------------------------------------------------------------------------

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *statusRecorder) WriteHeader(code int) { r.status = code; r.ResponseWriter.WriteHeader(code) }
func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		s.log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"bytes", rec.bytes, "dur_ms", time.Since(start).Milliseconds())
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return badRequest("invalid JSON body: %v", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// page is the envelope for every list endpoint. NextCursor is the id of the
// last item, or null when the page is not full (no more rows).
type page struct {
	Items      any     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// readPage parses ?cursor=<uuid>&limit=<1..200>. Limit defaults to 50.
func readPage(r *http.Request) (cursor string, limit int, err error) {
	q := r.URL.Query()
	limit = 50
	if q.Has("limit") {
		n, convErr := strconv.Atoi(q.Get("limit"))
		if convErr != nil || n < 1 || n > 200 {
			return "", 0, badRequest("limit must be 1..200")
		}
		limit = n
	}
	cursor = q.Get("cursor")
	if cursor != "" && !validUUID(cursor) {
		return "", 0, badRequest("cursor must be a uuid")
	}
	return cursor, limit, nil
}

func nextCursor(n, limit int, lastID string) *string {
	if n == limit {
		return &lastID
	}
	return nil
}

type httpError struct {
	status int
	msg    string
}

func (e *httpError) Error() string { return e.msg }

func badRequest(format string, args ...any) error {
	return &httpError{http.StatusBadRequest, fmt.Sprintf(format, args...)}
}

func notFound(what string) error { return &httpError{http.StatusNotFound, what + " not found"} }

// fail maps domain and storage errors onto HTTP semantics.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var (
		he      *httpError
		lineErr *ingest.LineError
		tooBig  *http.MaxBytesError
	)
	switch {
	case errors.As(err, &he):
		writeError(w, he.status, he.msg)
	case errors.As(err, &lineErr):
		writeError(w, http.StatusBadRequest, lineErr.Error())
	case errors.As(err, &tooBig):
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, domain.ErrInvalidTransition), storage.IsConflict(err):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, domain.ErrUnknownNode):
		writeError(w, http.StatusBadRequest, err.Error())
	case storage.IsBadReference(err):
		writeError(w, http.StatusUnprocessableEntity, "referenced entity does not exist in this tenant")
	default:
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
