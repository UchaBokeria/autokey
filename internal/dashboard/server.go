package dashboard

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"embed"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/uchabokeria/autokey/internal/onramp"
)

//go:embed dist
var distFS embed.FS

// Server serves the dashboard SPA + JSON API on one port.
type Server struct {
	DB      *sql.DB
	Bind    string
	Port    int
	Bearer  string
	AllowIP []string
	LogFile string
	Onramp  *onramp.Client
	Version string
}

func (s *Server) api() *API {
	return &API{DB: s.DB, Onramp: s.Onramp, Now: time.Now, Version: s.Version}
}

func (s *Server) authorize(r *http.Request) bool {
	if s.Bearer == "" {
		return false
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.Bearer)) != 1 {
		// Allow ?token= for plain <a>/fetch without headers (dashboard SPA).
		if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(s.Bearer)) != 1 {
			return false
		}
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
	line, _ := jsonMarshal(map[string]string{
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

// routes wires /api/* (auth) + SPA fallback (auth too — PAN-adjacent data).
func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	api := s.api()
	guard := func(h func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			if !s.authorize(r) {
				s.log("warn", "unauthorized dashboard from "+r.RemoteAddr)
				fail(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("/api/overview", guard(func(w http.ResponseWriter, r *http.Request) {
		out, err := api.Overview(r.Context())
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		ok(w, out)
	}))
	mux.HandleFunc("/api/cards", guard(func(w http.ResponseWriter, r *http.Request) {
		out, err := api.Cards(r.Context(), r.URL.Query().Get("provider"))
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		ok(w, map[string]any{"cards": out})
	}))
	mux.HandleFunc("/api/cards/", guard(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/cards/")
		if strings.HasSuffix(id, "/stats") {
			out, err := api.CardStats(r.Context(), strings.TrimSuffix(id, "/stats"))
			if err != nil {
				fail(w, http.StatusInternalServerError, err.Error())
				return
			}
			ok(w, map[string]any{"stats": out})
			return
		}
		fail(w, http.StatusNotFound, "unknown card route")
	}))
	mux.HandleFunc("/api/requests", guard(func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		out, err := api.Requests(r.Context(), limit)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		ok(w, map[string]any{"requests": out})
	}))
	mux.HandleFunc("/api/requests/", guard(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/requests/")
		if strings.HasSuffix(rest, "/keys") {
			out, err := api.RequestKeys(r.Context(), strings.TrimSuffix(rest, "/keys"))
			if err != nil {
				fail(w, http.StatusInternalServerError, err.Error())
				return
			}
			ok(w, map[string]any{"keys": out})
			return
		}
		if strings.HasSuffix(rest, "/email") {
			email, err := api.EmailForRequest(r.Context(), strings.TrimSuffix(rest, "/email"))
			if err != nil {
				fail(w, http.StatusNotFound, err.Error())
				return
			}
			ok(w, map[string]any{"email": email})
			return
		}
		fail(w, http.StatusNotFound, "unknown request route")
	}))
	mux.HandleFunc("/api/inbox", guard(func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		out, err := api.Inbox(r.Context(), limit, r.URL.Query().Get("recipient"))
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		ok(w, map[string]any{"emails": out})
	}))
	mux.HandleFunc("/api/inbox/", guard(func(w http.ResponseWriter, r *http.Request) {
		out, err := api.InboxMessage(r.Context(), strings.TrimPrefix(r.URL.Path, "/api/inbox/"))
		if err != nil {
			fail(w, http.StatusNotFound, err.Error())
			return
		}
		ok(w, out)
	}))
	mux.HandleFunc("/api/otp", guard(func(w http.ResponseWriter, r *http.Request) {
		email := r.URL.Query().Get("email")
		if email == "" {
			fail(w, http.StatusBadRequest, "email required")
			return
		}
		res, found, err := api.OTPFor(r.Context(), email)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !found {
			fail(w, http.StatusNotFound, "no code found")
			return
		}
		ok(w, map[string]any{
			"email": res.Recipient, "code": res.Code, "subject": res.Subject,
			"from": res.Sender, "received_at": res.ReceivedAt,
		})
	}))
	mux.HandleFunc("/api/onramp/stock", guard(func(w http.ResponseWriter, r *http.Request) {
		out, err := api.OnrampStock(r.Context())
		if err != nil {
			fail(w, http.StatusBadGateway, err.Error())
			return
		}
		ok(w, map[string]any{"stock": out})
	}))
	mux.HandleFunc("/api/cards-custom/", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			fail(w, http.StatusMethodNotAllowed, "DELETE only")
			return
		}
		if err := api.RemoveCustom(r.Context(), strings.TrimPrefix(r.URL.Path, "/api/cards-custom/")); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		ok(w, map[string]any{"success": true})
	}))
	mux.HandleFunc("/api/logs", guard(func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("lines"))
		if n <= 0 || n > 1000 {
			n = 100
		}
		lines, err := tailFile(s.LogFile, n)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		ok(w, map[string]any{"lines": lines})
	}))
	// SPA fallback: serve embedded dist, index.html for unknown paths.
	sub, _ := fs.Sub(distFS, "dist")
	mux.HandleFunc("/", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			if f, err := sub.Open(strings.TrimPrefix(r.URL.Path, "/")); err == nil {
				_ = f.Close()
				http.FileServer(http.FS(sub)).ServeHTTP(w, r)
				return
			}
		}
		http.ServeFileFS(w, r, sub, "index.html")
	}))
	return mux
}

// Serve starts the dashboard server.
func (s *Server) Serve(ctx context.Context) error {
	addr := net.JoinHostPort(s.Bind, strconv.Itoa(s.Port))
	srv := &http.Server{
		Addr: addr, Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	s.log("info", "dashboard listening on "+addr)
	return srv.ListenAndServe()
}
