package desktop

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/dcyber-lab/mad/internal/desktop/api"
)

// Every answer of the API says its version, whatever the route; the page
// and its assets do not.
func TestAPIAnswersCarryTheVersion(t *testing.T) {
	h := headers(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	for path, want := range map[string]string{
		"/api/deck":           strconv.Itoa(api.Version),
		"/api/bus/messages":   strconv.Itoa(api.Version),
		"/api/no-such-route":  strconv.Itoa(api.Version),
		"/":                   "",
		"/app.js":             "",
		"/apiary/not-the-api": "",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if got := rec.Header().Get(api.VersionHeader); got != want {
			t.Errorf("%s: %s %q, want %q", path, api.VersionHeader, got, want)
		}
	}
}
