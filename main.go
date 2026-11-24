package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"pingu/internal/monitor"
	"pingu/internal/server"
)

//go:embed web/*
var web embed.FS

func main() {
	addr := env("PINGU_ADDR", "127.0.0.1:8080")
	token := os.Getenv("PINGU_TOKEN")
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		log.Fatal("PINGU_ADDR must be host:port: ", err)
	}
	ip := net.ParseIP(host)
	if (ip == nil || !ip.IsLoopback()) && len(token) < 24 {
		log.Fatal("외부 주소로 실행하려면 24자 이상의 PINGU_TOKEN이 필요합니다")
	}
	probe := monitor.NewProber(os.Getenv("PINGU_ALLOW_PRIVATE") == "true")
	defer probe.Close()
	engine, err := monitor.NewEngine(env("PINGU_DATA", "data/jobs.json"), probe.Check)
	if err != nil {
		log.Fatal(err)
	}
	defer engine.Close()
	assets, _ := fs.Sub(web, "web")
	h := server.New(engine, assets, token, probe.ICMPStatus())
	s := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(shutdown)
	}()
	log.Printf("Pingu → http://%s (DB 없음)", addr)
	if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
