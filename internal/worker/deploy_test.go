package worker

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mockCF(t *testing.T) (Deps, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"success":false,"errors":[{"code":1,"message":"bad auth"}]}`)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/email/routing/dns") && r.Method == "POST":
			_, _ = io.WriteString(w, `{"success":true,"result":[]}`)
		case strings.HasSuffix(r.URL.Path, "/workers/scripts/autokey-inbox") && r.Method == "PUT":
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"success":false,"errors":[{"code":2,"message":"want multipart"}]}`)
				return
			}
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "async email(") {
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"success":false,"errors":[{"code":5,"message":"want email handler"}]}`)
				return
			}
			_, _ = io.WriteString(w, `{"success":true,"result":{}}`)
		case strings.HasSuffix(r.URL.Path, "/email/routing/addresses") && r.Method == "POST":
			_, _ = io.WriteString(w, `{"success":true,"result":{"email":"g@gmail.com"}}`)
		case strings.HasSuffix(r.URL.Path, "/rules/catch_all") && r.Method == "PUT":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			acts, _ := body["actions"].([]any)
			if len(acts) != 1 {
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"success":false,"errors":[{"code":3,"message":"want 1 action"}]}`)
				return
			}
			a0, _ := acts[0].(map[string]any)
			if a0["type"] != "worker" {
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"success":false,"errors":[{"code":6,"message":"want worker action"}]}`)
				return
			}
			_, _ = io.WriteString(w, `{"success":true,"result":{}}`)
		case strings.HasSuffix(r.URL.Path, "/email/routing") && r.Method == "GET":
			_, _ = io.WriteString(w, `{"success":true,"result":{"enabled":true,"status":"ready"}}`)
		case strings.HasSuffix(r.URL.Path, "/rules/catch_all") && r.Method == "GET":
			_, _ = io.WriteString(w, `{"success":true,"result":{"enabled":true,"actions":[{"type":"worker","value":["autokey-inbox"]}]}}`)
		default:
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"success":false,"errors":[{"code":4,"message":"nope"}]}`)
		}
	}))
	t.Cleanup(srv.Close)
	d := New("tok", "acct", "zone", "autokey-inbox")
	d.Base = srv.URL
	return d, &calls
}

func TestTemplateNoImports(t *testing.T) {
	if strings.Contains(Template(), "from \"") || strings.Contains(Template(), "from '") {
		t.Fatal("template must be dependency-free for raw REST upload")
	}
	if !strings.Contains(Template(), "async email(") {
		t.Fatal("template must define email() handler")
	}
}

func TestDeployChain(t *testing.T) {
	ctx := t.Context()
	d, calls := mockCF(t)
	if err := d.EnableRoutingDNS(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.UploadScript(ctx, "http://127.0.0.1:8765/v1/inbox", "bearer", "g@gmail.com"); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateDestination(ctx, "g@gmail.com"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetCatchAllWorker(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"POST /zones/zone/email/routing/dns",
		"PUT /accounts/acct/workers/scripts/autokey-inbox",
		"POST /accounts/acct/email/routing/addresses",
		"PUT /zones/zone/email/routing/rules/catch_all",
	}
	if len(*calls) != len(want) {
		t.Fatalf("calls=%v", *calls)
	}
	for i := range want {
		if (*calls)[i] != want[i] {
			t.Fatalf("call %d = %q want %q", i, (*calls)[i], want[i])
		}
	}
}

func TestStatus(t *testing.T) {
	d, _ := mockCF(t)
	routing, catchAll, err := d.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(routing, "ready") {
		t.Fatalf("routing=%q", routing)
	}
	if !strings.Contains(catchAll, "autokey-inbox") {
		t.Fatalf("catchall=%q", catchAll)
	}
}

func TestAuthError(t *testing.T) {
	d, _ := mockCF(t)
	d.APIToken = "wrong"
	if err := d.EnableRoutingDNS(t.Context()); err == nil {
		t.Fatal("want auth error")
	}
}
