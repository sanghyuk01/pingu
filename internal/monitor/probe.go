package monitor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

var denied = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("2001::/32"),
}

func allowed(ip netip.Addr, private bool) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	if private {
		return true
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range denied {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

type waiting struct {
	ip netip.Addr
	ch chan struct{}
}
type echoSocket struct {
	conn    *icmp.PacketConn
	proto   int
	raw     bool
	mu      sync.Mutex
	pending map[string]waiting
	done    chan struct{}
}
type Prober struct {
	client       *http.Client
	transport    *http.Transport
	allowPrivate bool
	v4, v6       *echoSocket
}

func NewProber(private bool) *Prober {
	p := &Prober{allowPrivate: private}
	p.v4 = openEcho("udp4", "0.0.0.0", 1)
	p.v6 = openEcho("udp6", "::", 58)
	p.transport = &http.Transport{Proxy: nil, DialContext: p.dial, ForceAttemptHTTP2: true, MaxConnsPerHost: 4, MaxIdleConns: 32, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 16 << 10, DisableCompression: true}
	p.client = &http.Client{Transport: p.transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return p
}
func openEcho(network, addr string, proto int) *echoSocket {
	c, err := icmp.ListenPacket(network, addr)
	raw := false
	if err != nil {
		if proto == 1 {
			network = "ip4:icmp"
		} else {
			network = "ip6:ipv6-icmp"
		}
		c, err = icmp.ListenPacket(network, addr)
		raw = true
	}
	if err != nil {
		return nil
	}
	s := &echoSocket{conn: c, proto: proto, raw: raw, pending: map[string]waiting{}, done: make(chan struct{})}
	go s.read()
	return s
}
func (s *echoSocket) read() {
	defer close(s.done)
	b := make([]byte, 1500)
	for {
		n, peer, err := s.conn.ReadFrom(b)
		if err != nil {
			return
		}
		m, err := icmp.ParseMessage(s.proto, b[:n])
		if err != nil {
			continue
		}
		if (s.proto == 1 && m.Type != ipv4.ICMPTypeEchoReply) || (s.proto == 58 && m.Type != ipv6.ICMPTypeEchoReply) {
			continue
		}
		echo, ok := m.Body.(*icmp.Echo)
		if !ok {
			continue
		}
		var source netip.Addr
		switch a := peer.(type) {
		case *net.IPAddr:
			source, _ = netip.AddrFromSlice(a.IP)
		case *net.UDPAddr:
			source, _ = netip.AddrFromSlice(a.IP)
		}
		key := string(echo.Data)
		s.mu.Lock()
		w, ok := s.pending[key]
		if ok && source.Unmap() == w.ip.Unmap() {
			delete(s.pending, key)
			close(w.ch)
		}
		s.mu.Unlock()
	}
}
func (p *Prober) ICMPStatus() map[string]bool {
	return map[string]bool{"ipv4": p.v4 != nil, "ipv6": p.v6 != nil}
}
func (p *Prober) Close() {
	p.transport.CloseIdleConnections()
	for _, s := range []*echoSocket{p.v4, p.v6} {
		if s != nil {
			_ = s.conn.Close()
			<-s.done
		}
	}
}
func (p *Prober) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else {
		var err error
		ips, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
	}
	if len(ips) == 0 {
		return nil, errors.New("DNS 주소를 찾을 수 없어요")
	}
	for _, ip := range ips {
		if !allowed(ip, p.allowPrivate) {
			return nil, errors.New("허용되지 않은 IP예요. 내부망 검사는 서버 설정에서 허용해야 해요")
		}
	}
	return ips, nil
}
func (p *Prober) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := p.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	d := net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	var last error
	for _, ip := range ips {
		c, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return c, nil
		}
		last = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, last
}
func (p *Prober) Check(ctx context.Context, kind, address string) Result {
	start := time.Now()
	var r Result
	var err error
	if kind == "http" {
		r.Code, err = p.http(ctx, address)
		r.OK = err == nil && r.Code >= 200 && r.Code < 300
		if err == nil && !r.OK {
			r.Error = "HTTP " + strconv.Itoa(r.Code)
		}
	} else {
		err = p.echo(ctx, address)
		r.OK = err == nil
	}
	r.Latency = float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			r.Error = "응답 제한시간 초과"
		} else {
			r.Error = err.Error()
		}
		if len(r.Error) > 300 {
			r.Error = r.Error[:300]
		}
	}
	return r
}
func (p *Prober) http(ctx context.Context, address string) (int, error) {
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		req, err := http.NewRequestWithContext(ctx, method, address, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("User-Agent", "Pingu/0.1 (uptime monitor)")
		resp, err := p.client.Do(req)
		if err != nil {
			return 0, err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if method == http.MethodHead && (resp.StatusCode == 405 || resp.StatusCode == 501) {
			continue
		}
		return resp.StatusCode, nil
	}
	return 0, errors.New("HTTP 응답을 읽을 수 없어요")
}
func (p *Prober) echo(ctx context.Context, host string) error {
	ips, err := p.resolve(ctx, host)
	if err != nil {
		return err
	}
	ip := ips[0].Unmap()
	s := p.v6
	typ := icmp.Type(ipv6.ICMPTypeEchoRequest)
	if ip.Is4() {
		s = p.v4
		typ = ipv4.ICMPTypeEcho
	}
	if s == nil {
		return errors.New("ICMP 소켓을 사용할 수 없어요. 실행 권한을 확인해 주세요")
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	payload := []byte(hex.EncodeToString(nonce[:]))
	w := waiting{ip: ip, ch: make(chan struct{})}
	key := string(payload)
	s.mu.Lock()
	s.pending[key] = w
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, key); s.mu.Unlock() }()
	m := icmp.Message{Type: typ, Code: 0, Body: &icmp.Echo{ID: 0x5047, Seq: int(nonce[0])<<8 | int(nonce[1]), Data: payload}}
	b, err := m.Marshal(nil)
	if err != nil {
		return err
	}
	var dst net.Addr = &net.UDPAddr{IP: net.ParseIP(ip.String())}
	if s.raw {
		dst = &net.IPAddr{IP: net.ParseIP(ip.String())}
	}
	if _, err = s.conn.WriteTo(b, dst); err != nil {
		return fmt.Errorf("ICMP 전송 실패: %w", err)
	}
	select {
	case <-w.ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return errors.New("ICMP 소켓이 닫혔어요")
	}
}
