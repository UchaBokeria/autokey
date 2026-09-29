package omegameta

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// fixture serves a miniature omegameta: signup (email+password+DOB
// selects), billing (country+card), api-keys (create → sk-test-N).
// No live hits; exercises the real profile step functions.
func fixture(t *testing.T) string {
	t.Helper()
	var n int
	mux := http.NewServeMux()
	mux.HandleFunc("/signup", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<!doctype html><html><body>
<form>
<label>Email <input type="email" name="email"></label>
<button type="submit">Continue</button>
<label>Password <input type="password" name="password"></label>
<label>Confirm password <input type="password" name="confirm"></label>
<label><input type="checkbox" name="remember"> Remember me</label>
<label>Day <select name="dob-day"><option>1</option><option>27</option></select></label>
<label>Month <select name="dob-month"><option>1</option><option>9</option></select></label>
<label>Year <select name="dob-year"><option>2006</option><option>1990</option></select></label>
<button type="submit">Create account</button>
</form></body></html>`))
	})
	mux.HandleFunc("/billing", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<!doctype html><html><body>
<button>Add payment method</button>
<form>
<label>Country <select name="country"><option>US</option><option>GB</option></select></label>
<label>Card number <input name="number"></label>
<label>Expiry <input name="exp"></label>
<label>CVC <input name="cvc"></label>
<label>Name on card <input name="cardname"></label>
<button type="submit">Save</button>
</form>
<div>Payment method added, ending in 1111</div>
</body></html>`))
	})
	mux.HandleFunc("/api-keys", func(w http.ResponseWriter, r *http.Request) {
		n++
		_, _ = w.Write([]byte(`<!doctype html><html><body>
<button>Create key</button>
<div aria-label="API key">sk-test-KEY</div>
</body></html>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}
