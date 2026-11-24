package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"

	"pingu/internal/monitor"
)

func New(e *monitor.Engine, assets fs.FS, token string, icmp map[string]bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]bool{"ok": true}) })
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"icmp": icmp, "version": "0.1.0", "storage": "memory", "auth": token != ""})
	})
	mux.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(e.JSON())
	})
	mux.HandleFunc("POST /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			fail(w, 415, "JSON 형식으로 요청해 주세요")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		var spec monitor.Spec
		if err := d.Decode(&spec); err != nil {
			fail(w, 400, "입력 내용을 확인해 주세요 (최대 64KB)")
			return
		}
		if d.Decode(&struct{}{}) != io.EOF {
			fail(w, 400, "JSON 요청 하나만 보낼 수 있어요")
			return
		}
		id, err := e.Create(spec)
		if err != nil {
			if errors.Is(err, monitor.ErrStorage) {
				fail(w, 500, monitor.ErrStorage.Error())
				return
			}
			fail(w, 400, err.Error())
			return
		}
		reply(w, 201, map[string]string{"id": id})
	})
	mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		b, err := e.JobJSON(r.PathValue("id"))
		if err != nil {
			fail(w, 404, "작업을 찾을 수 없어요")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	})
	mux.HandleFunc("POST /api/jobs/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		if err := e.Stop(r.PathValue("id"), false); err != nil {
			operationError(w, err)
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("DELETE /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := e.Stop(r.PathValue("id"), true); err != nil {
			operationError(w, err)
			return
		}
		w.WriteHeader(204)
	})
	files := http.FileServer(http.FS(assets))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/app.js" && r.URL.Path != "/theme.js" && r.URL.Path != "/style.css" && r.URL.Path != "/penguin.svg" {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if token != "" {
				got := sha256.Sum256([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
				want := sha256.Sum256([]byte(token))
				if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
					fail(w, 401, "서버 접속 토큰을 입력해 주세요")
					return
				}
			}
			if r.Method != "GET" && r.Method != "HEAD" {
				if origin := r.Header.Get("Origin"); origin != "" {
					u, err := url.Parse(origin)
					if err != nil || u.Host != r.Host {
						fail(w, 403, "같은 서버 화면에서 요청해 주세요")
						return
					}
				}
				if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
					fail(w, 403, "외부 사이트의 요청은 허용하지 않아요")
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	reply(w, status, map[string]string{"error": message})
}
func operationError(w http.ResponseWriter, err error) {
	if errors.Is(err, os.ErrNotExist) {
		fail(w, 404, "작업을 찾을 수 없어요")
	} else {
		fail(w, 500, "작업 설정을 저장할 수 없어요")
	}
}
