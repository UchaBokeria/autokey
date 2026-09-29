package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/uchabokeria/autokey/internal/automate"
	"github.com/uchabokeria/autokey/internal/flow"
)

// generateAsync runs GenerateDetails in background (202 immediately).
// With stream=true it additionally streams request_steps as SSE until done.
func (s *Server) generateAsync(w http.ResponseWriter, r *http.Request, provider, service string, qty int, stream bool, proxy, captcha string) {
	deps := s.Deps
	if service != "" {
		deps.Service = service
	}
	done := make(chan asyncResult, 1)
	//nolint:gosec // background job must outlive the request context by design
	go func() {
		ctx := automate.WithOverrides(context.Background(), proxy, captcha)
		detail, err := flow.GenerateDetails(ctx, deps, provider, qty)
		keys := []string{}
		if err == nil {
			rows, qerr := s.DB.QueryContext(ctx,
				`SELECT key_value FROM keys WHERE request_id=(SELECT id FROM requests WHERE email=?)`, detail.Email)
			if qerr == nil {
				defer func() { _ = rows.Close() }()
				for rows.Next() {
					var k string
					_ = rows.Scan(&k)
					keys = append(keys, k)
				}
			}
		}
		done <- asyncResult{detail: detail, keys: keys, err: err}
	}()
	if !stream {
		// Fire-and-forget is useless without an id; wait briefly for the
		// request row, then return 202 with email for polling.
		select {
		case res := <-done:
			s.writeResult(w, res)
			return
		case <-time.After(2 * time.Second):
		}
		// Still running: find the pending request by provider+qty recency.
		var email, reqID string
		_ = s.DB.QueryRowContext(r.Context(), `
SELECT email,id FROM requests WHERE provider=? AND key_quantity=?
ORDER BY created_at DESC LIMIT 1`, provider, qty).Scan(&email, &reqID)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"success": true, "accepted": true,
			"email": email, "request_id": reqID,
			"poll": "/v1/requests/" + reqID,
		})
		return
	}
	s.streamProgress(w, r, done)
}

type asyncResult struct {
	detail flow.Details
	keys   []string
	err    error
}

func (s *Server) writeResult(w http.ResponseWriter, res asyncResult) {
	if res.err != nil {
		s.log("warn", "generate failed: "+res.err.Error())
		writeJSON(w, http.StatusBadGateway, map[string]any{"success": false, "message": res.err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true, "email": res.detail.Email,
		"card_last4": res.detail.Last4, "provider": res.detail.Provider, "keys": res.keys,
	})
}

// streamProgress emits request_steps rows as SSE until the run finishes.
// WriteTimeout stays 0 on this route so proxies don't kill long runs.
func (s *Server) streamProgress(w http.ResponseWriter, r *http.Request, done <-chan asyncResult) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "streaming unsupported"})
		return
	}
	seen := 0
	// Resolve request email once the row exists.
	var email string
	for i := 0; i < 40 && email == ""; i++ {
		_ = s.DB.QueryRowContext(r.Context(),
			`SELECT email FROM requests ORDER BY created_at DESC LIMIT 1`).Scan(&email)
		if email == "" {
			select {
			case res := <-done:
				s.writeResult(w, res)
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	emit := func(event, data string) {
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		flusher.Flush()
	}
	for {
		select {
		case res := <-done:
			raw, _ := json.Marshal(map[string]any{
				"success": res.err == nil, "email": res.detail.Email,
				"card_last4": res.detail.Last4, "provider": res.detail.Provider,
				"keys": res.keys, "error": errStr(res.err),
			})
			emit("result", string(raw))
			return
		case <-time.After(2 * time.Second):
		}
		if email == "" {
			continue
		}
		rows, err := s.DB.QueryContext(r.Context(), `
SELECT rs.step,rs.ok,rs.detail FROM request_steps rs
JOIN requests r ON r.id=rs.request_id
WHERE r.email=? ORDER BY rs.id LIMIT -1 OFFSET ?`, email, seen)
		if err != nil {
			continue
		}
		count := 0
		for rows.Next() {
			var step string
			var ok int
			var detail string
			_ = rows.Scan(&step, &ok, &detail)
			raw, _ := json.Marshal(map[string]any{"step": step, "ok": ok != 0, "detail": detail})
			emit("step", string(raw))
			count++
		}
		_ = rows.Close()
		seen += count
	}
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// requestDetail handles GET /v1/requests/{id}: status + steps + keys.
func (s *Server) requestDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "message": "GET only"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/requests/")
	if id == "" || strings.Contains(id, "/") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "request id required"})
		return
	}
	var email, service, provider, status, cardID, errMsg, created, updated string
	var qty int
	err := s.DB.QueryRowContext(r.Context(), `
SELECT email,service,key_quantity,provider,status,COALESCE(card_id,''),COALESCE(error,''),created_at,updated_at
FROM requests WHERE id=?`, id).Scan(&email, &service, &qty, &provider, &status, &cardID, &errMsg, &created, &updated)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "request not found"})
		return
	}
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT key_value FROM keys WHERE request_id=? ORDER BY created_at`, id)
	keys := []string{}
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var k string
			_ = rows.Scan(&k)
			keys = append(keys, k)
		}
	}
	srows, err := s.DB.QueryContext(r.Context(),
		`SELECT step,ok,detail,created_at FROM request_steps WHERE request_id=? ORDER BY id`, id)
	steps := []map[string]any{}
	if err == nil {
		defer func() { _ = srows.Close() }()
		for srows.Next() {
			var step, detail, at string
			var ok int
			_ = srows.Scan(&step, &ok, &detail, &at)
			steps = append(steps, map[string]any{"step": step, "ok": ok != 0, "detail": detail, "at": at})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true, "id": id, "email": email, "service": service,
		"key_quantity": qty, "provider": provider, "status": status,
		"card_id": cardID, "error": errMsg, "created_at": created,
		"updated_at": updated, "keys": keys, "steps": steps,
	})
}
