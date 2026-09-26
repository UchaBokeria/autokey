package hook

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uchabokeria/autokey/internal/flow"
)

// Server is the autokey hook HTTP server.
type Server struct {
	DB      *sql.DB
	Deps    flow.Deps
	Bind    string
	Port    int
	Bearer  string
	AllowIP []string
	LogFile string
}

// bearerAuth enforces the bearer matrix (spec §9: bearer baseline PROPOSED,
// IP allowlist optional, mTLS optional at TLS layer).
func (s *Server) authorize(r *http.Request) bool {
	if s.Bearer == "" {
		return false
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.Bearer)) != 1 {
		return false
	}
	if len(s.AllowIP) == 0 {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	for _, allow := range s.AllowIP {
		if _, cidr, err := net.ParseCIDR(allow); err == nil {
			if cidr.Contains(ip) {
				return true
			}
			continue
		}
		if allow == host {
			return true
		}
	}
	return false
}

func (s *Server) log(kind, msg string) {
	line, _ := json.Marshal(map[string]string{
		"at":   time.Now().UTC().Format(time.RFC3339),
		"kind": kind, "msg": msg,
	})
	if s.LogFile != "" {
		_ = os.MkdirAll(filepath.Dir(s.LogFile), 0o700)
		if f, err := os.OpenFile(s.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			_, _ = f.Write(append(line, '\n'))
			_ = f.Close()
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// generate handles POST /v1/generate {"keyQuantity": N}.
func (s *Server) generate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "message": "POST only"})
		return
	}
	if !s.authorize(r) {
		s.log("warn", "unauthorized /v1/generate from "+r.RemoteAddr)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "message": "unauthorized"})
		return
	}
	var req struct {
		KeyQuantity int    `json:"keyQuantity"`
		Provider    string `json:"provider"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "invalid JSON"})
		return
	}
	if req.KeyQuantity < 1 || req.KeyQuantity > 100 { // PROPOSED max 100, confirm (spec §18)
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "keyQuantity must be 1..100"})
		return
	}
	detail, err := flow.GenerateDetails(r.Context(), s.Deps, req.Provider, req.KeyQuantity)
	if err != nil {
		s.log("warn", "generate failed: "+err.Error())
		writeJSON(w, http.StatusBadGateway, map[string]any{"success": false, "message": err.Error()})
		return
	}
	var keys []string
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT key_value FROM keys WHERE request_id=(SELECT id FROM requests WHERE email=?)`, detail.Email)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var k string
			_ = rows.Scan(&k)
			keys = append(keys, k)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true, "email": detail.Email,
		"card_last4": detail.Last4, "provider": detail.Provider, "keys": keys,
	})
}

// inbox handles POST /v1/inbox from the Cloudflare Worker.
func (s *Server) inbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "message": "POST only"})
		return
	}
	if !s.authorize(r) {
		s.log("warn", "unauthorized /v1/inbox from "+r.RemoteAddr)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "message": "unauthorized"})
		return
	}
	var m struct {
		EnvelopeTo string `json:"envelope_to"`
		From       string `json:"from"`
		Subject    string `json:"subject"`
		Text       string `json:"text"`
		HTML       string `json:"html"`
		MessageID  string `json:"message_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&m); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "invalid JSON"})
		return
	}
	if m.MessageID == "" || m.EnvelopeTo == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "message_id and envelope_to required"})
		return
	}
	_, err := s.DB.ExecContext(r.Context(), `
INSERT OR IGNORE INTO inbound_emails(message_id,recipient,sender,subject,body_text,body_html,received_at)
VALUES(?,?,?,?,?,?,?)`, m.MessageID, strings.ToLower(m.EnvelopeTo), m.From, m.Subject, m.Text, m.HTML,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "stored"})
}

// Serve starts the hook server.
func (s *Server) Serve(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/generate", s.generate)
	mux.HandleFunc("/v1/inbox", s.inbox)
	addr := fmt.Sprintf("%s:%d", s.Bind, s.Port)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	s.log("info", "hook listening on "+addr)
	return srv.ListenAndServe()
}
