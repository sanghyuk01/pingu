package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"pingu/internal/monitor"
)

func TestAPIAuthOriginLifecycle(t *testing.T) {
	e, err := monitor.NewEngine(filepath.Join(t.TempDir(), "jobs.json"), func(context.Context, string, string) monitor.Result { return monitor.Result{OK: true} })
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	h := New(e, fstest.MapFS{"index.html": {Data: []byte("pingu")}}, "test-token", map[string]bool{"ipv4": true})
	call := func(method, path, body, token, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/api/jobs", "", "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	body := `{"name":"=cmd","addresses":["1.1.1.1"],"interval":10,"duration":60,"timeout":2}`
	if w := call("POST", "/api/jobs", body, "test-token", "https://evil.example"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	w := call("POST", "/api/jobs", body, "test-token", "")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var created map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	id := created["id"]
	if w = call("POST", "/api/jobs/"+id+"/stop", "", "test-token", ""); w.Code != 200 {
		t.Fatal("stop failed")
	}
	if w = call("DELETE", "/api/jobs/"+id, "", "test-token", ""); w.Code != 204 {
		t.Fatal("delete failed")
	}
	if w = call("GET", "/api/jobs/"+id, "", "test-token", ""); w.Code != 404 {
		t.Fatal("deleted job remains")
	}
	if w = call("GET", "/", "", "", ""); w.Code != http.StatusOK {
		t.Fatal("UI requires token before login")
	}
}
