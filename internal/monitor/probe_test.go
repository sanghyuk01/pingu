package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

func TestAddressPolicy(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "10.0.0.1", "169.254.169.254", "100.100.100.200", "0.0.0.0", "224.0.0.1", "192.0.2.1", "64:ff9b::a9fe:a9fe", "2002:7f00:1::"} {
		if allowed(netip.MustParseAddr(s), false) {
			t.Errorf("allowed %s", s)
		}
	}
	if !allowed(netip.MustParseAddr("1.1.1.1"), false) {
		t.Fatal("public IP blocked")
	}
}
func TestHTTPFallbackAndNoRedirect(t *testing.T) {
	var heads, gets, redirected atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/private", 302)
			return
		}
		if r.URL.Path == "/private" {
			redirected.Add(1)
		}
		if r.Method == "HEAD" {
			heads.Add(1)
			w.WriteHeader(405)
			return
		}
		gets.Add(1)
		w.WriteHeader(204)
	}))
	defer s.Close()
	p := NewProber(true)
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r := p.Check(ctx, "http", s.URL)
	if !r.OK || r.Code != 204 || heads.Load() != 1 || gets.Load() != 1 {
		t.Fatalf("fallback: %+v", r)
	}
	r = p.Check(ctx, "http", s.URL+"/redirect")
	if r.OK || r.Code != 302 || redirected.Load() != 0 {
		t.Fatalf("followed redirect: %+v", r)
	}
	public := NewProber(false)
	defer public.Close()
	r = public.Check(ctx, "http", s.URL)
	if r.OK || r.Error == "" {
		t.Fatal("localhost bypassed address policy")
	}
}
func TestICMPLoopback(t *testing.T) {
	p := NewProber(true)
	defer p.Close()
	if p.v4 == nil {
		t.Skip("ICMP socket unavailable on this host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := p.Check(ctx, "icmp", "127.0.0.1")
	if !r.OK {
		t.Fatalf("ICMP echo reply not received: %+v", r)
	}
}
