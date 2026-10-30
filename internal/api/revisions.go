package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/domain"
)

// revisableTypes are the record types that keep revisions (SPEC-007-R07).
var revisableTypes = map[string]bool{"lineage_node": true, "run": true, "experiment": true, "note": true}

// recordPath reads {type} and {id} from the route. An unknown type or a
// malformed id is not found.
func recordPath(r *http.Request) (string, string, error) {
	typ, id := r.PathValue("type"), r.PathValue("id")
	if !revisableTypes[typ] || !validUUID(id) {
		return "", "", notFound("record")
	}
	return typ, id, nil
}

// positiveInt parses a revision number. Revisions count from 1.
func positiveInt(s, name string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, badRequest("%s must be a positive integer", name)
	}
	return n, nil
}

// loadRevisions reads every revision of one record, oldest first. A record
// with no revisions is not found.
func loadRevisions(ctx context.Context, tx pgx.Tx, typ, id string) ([]domain.Revision, error) {
	rows, err := tx.Query(ctx, `
		SELECT rev, value, author, reason, change_set_id, created_at FROM record_revisions
		WHERE entity_type = $1 AND entity_id = $2 ORDER BY rev`, typ, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	revs := []domain.Revision{}
	for rows.Next() {
		var rev domain.Revision
		if err := rows.Scan(&rev.Rev, &rev.Value, &rev.Author, &rev.Reason, &rev.ChangeSetID, &rev.CreatedAt); err != nil {
			return nil, err
		}
		revs = append(revs, rev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(revs) == 0 {
		return nil, notFound("record")
	}
	return revs, nil
}

// findRevision returns the revision numbered rev from revs.
func findRevision(revs []domain.Revision, rev int) (domain.Revision, error) {
	for _, r := range revs {
		if r.Rev == rev {
			return r, nil
		}
	}
	return domain.Revision{}, notFound("revision")
}

// listRevisions returns every revision of a record, oldest first.
//
//	GET /v1/records/{type}/{id}/revisions
func (s *Server) listRevisions(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	typ, id, err := recordPath(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var revs []domain.Revision
	err = s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) (err error) {
		revs, err = loadRevisions(ctx, tx, typ, id)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": revs})
}

// getRevision returns one revision of a record.
//
//	GET /v1/records/{type}/{id}/revisions/{rev}
func (s *Server) getRevision(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	typ, id, err := recordPath(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	n, err := positiveInt(r.PathValue("rev"), "rev")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var rev domain.Revision
	err = s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) error {
		revs, err := loadRevisions(ctx, tx, typ, id)
		if err != nil {
			return err
		}
		rev, err = findRevision(revs, n)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rev)
}

// diffRevisions compares two revisions of a record field by field.
//
//	GET /v1/records/{type}/{id}/diff?from=<rev>&to=<rev>
func (s *Server) diffRevisions(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	typ, id, err := recordPath(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	from, err := positiveInt(r.URL.Query().Get("from"), "from")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	to, err := positiveInt(r.URL.Query().Get("to"), "to")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var changes []domain.Change
	err = s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) error {
		revs, err := loadRevisions(ctx, tx, typ, id)
		if err != nil {
			return err
		}
		a, err := findRevision(revs, from)
		if err != nil {
			return err
		}
		b, err := findRevision(revs, to)
		if err != nil {
			return err
		}
		changes = domain.Diff(a.Value, b.Value)
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if changes == nil {
		changes = []domain.Change{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "changes": changes})
}

// blameRecord attributes each field of a record's latest value to the
// revision that last changed it.
//
//	GET /v1/records/{type}/{id}/blame
func (s *Server) blameRecord(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	typ, id, err := recordPath(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var revs []domain.Revision
	err = s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) (err error) {
		revs, err = loadRevisions(ctx, tx, typ, id)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rev":    revs[len(revs)-1].Rev,
		"fields": domain.Blame(revs),
	})
}
