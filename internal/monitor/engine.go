package monitor

import (
	"container/heap"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const MaxTargets = 1000
const MaxRate = 20.0
const MaxConcurrent = 64
const HistorySize = 60

var ErrStorage = errors.New("작업 설정을 저장할 수 없어요")

type Spec struct {
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
	Interval  int      `json:"interval"`
	Duration  int      `json:"duration"`
	Timeout   int      `json:"timeout"`
}
type Result struct {
	At      time.Time `json:"at"`
	OK      bool      `json:"ok"`
	Latency float64   `json:"latency"`
	Code    int       `json:"code,omitempty"`
	Error   string    `json:"error,omitempty"`
}
type Event struct {
	Result
	Address string `json:"address"`
	Kind    string `json:"kind"`
	Job     string `json:"job"`
}
type Target struct {
	Address string   `json:"address"`
	Kind    string   `json:"kind"`
	Count   int      `json:"count"`
	Success int      `json:"success"`
	Skipped int      `json:"skipped"`
	TotalMS float64  `json:"total_ms"`
	MinMS   float64  `json:"min_ms"`
	MaxMS   float64  `json:"max_ms"`
	History []Result `json:"history"`
	busy    bool
}
type Job struct {
	ID         string     `json:"id"`
	Spec       Spec       `json:"spec"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	EndsAt     time.Time  `json:"ends_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Targets    []*Target  `json:"targets"`
	ctx        context.Context
	cancel     context.CancelFunc
}
type ProbeFunc func(context.Context, string, string) Result
type task struct {
	at     time.Time
	job    *Job
	target *Target
}
type schedule []*task

func (s schedule) Len() int           { return len(s) }
func (s schedule) Less(i, j int) bool { return s[i].at.Before(s[j].at) }
func (s schedule) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s *schedule) Push(v any)        { *s = append(*s, v.(*task)) }
func (s *schedule) Pop() any {
	old := *s
	v := old[len(old)-1]
	old[len(old)-1] = nil
	*s = old[:len(old)-1]
	return v
}

type Engine struct {
	mu         sync.Mutex
	jobs       map[string]*Job
	queue      schedule
	path       string
	probe      ProbeFunc
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	slots      chan struct{}
	retryAfter time.Time
	recent     []Event
}
type saved struct {
	ID         string     `json:"id"`
	Spec       Spec       `json:"spec"`
	CreatedAt  time.Time  `json:"created_at"`
	EndsAt     time.Time  `json:"ends_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Status     string     `json:"status"`
}

func Validate(s Spec) (Spec, error) {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		s.Name = "새로운 탐험"
	}
	if len([]rune(s.Name)) > 80 {
		return s, errors.New("작업 이름은 80자 이내로 입력해 주세요")
	}
	if s.Interval < 1 || s.Interval > 86400 {
		return s, errors.New("검사 간격은 1~86,400초로 설정해 주세요")
	}
	if s.Duration < 1 || s.Duration > 86400 {
		return s, errors.New("실행 시간은 1초~24시간으로 설정해 주세요")
	}
	if s.Timeout < 1 || s.Timeout > 10 {
		return s, errors.New("응답 제한시간은 1~10초로 설정해 주세요")
	}
	if len(s.Addresses) < 1 || len(s.Addresses) > MaxTargets {
		return s, errors.New("대상은 1~1,000개 등록할 수 있어요")
	}
	seen := map[string]bool{}
	addresses := []string{}
	for _, a := range s.Addresses {
		a = strings.TrimSpace(a)
		if len(a) > 2048 || a == "" {
			return s, errors.New("비어 있거나 너무 긴 주소가 있어요")
		}
		if strings.Contains(a, "://") {
			u, err := url.Parse(a)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
				return s, fmt.Errorf("올바른 HTTP·HTTPS URL을 입력해 주세요: %s", a)
			}
			if p := u.Port(); p != "" {
				var n int
				if _, err := fmt.Sscanf(p, "%d", &n); err != nil || n < 1 || n > 65535 {
					return s, errors.New("올바른 포트 번호를 입력해 주세요")
				}
			}
		} else if net.ParseIP(a) == nil {
			if len(a) > 253 || strings.ContainsAny(a, " /:@?#\\\t\r\n") {
				return s, fmt.Errorf("올바른 IP 또는 호스트 이름을 입력해 주세요: %s", a)
			}
			for _, label := range strings.Split(a, ".") {
				if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
					return s, errors.New("호스트 이름 형식을 확인해 주세요")
				}
				for _, c := range label {
					if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
						return s, errors.New("호스트 이름은 영문·숫자·하이픈으로 입력해 주세요")
					}
				}
			}
		}
		if !seen[a] {
			addresses = append(addresses, a)
			seen[a] = true
		}
	}
	s.Addresses = addresses
	if float64(len(addresses))/float64(s.Interval) > MaxRate {
		return s, errors.New("전체 검사량은 초당 20회 이하여야 해요. 간격을 늘려 주세요")
	}
	return s, nil
}

func NewEngine(path string, probe ProbeFunc) (*Engine, error) {
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{jobs: map[string]*Job{}, path: path, probe: probe, ctx: ctx, cancel: cancel, slots: make(chan struct{}, MaxConcurrent)}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		cancel()
		return nil, err
	}
	if err == nil {
		var rows []saved
		if err = json.Unmarshal(b, &rows); err != nil {
			cancel()
			return nil, fmt.Errorf("작업 설정 파일을 읽을 수 없어요: %w", err)
		}
		count := 0
		for _, r := range rows {
			if len(e.jobs) >= 100 {
				cancel()
				return nil, errors.New("저장된 작업이 100개를 초과합니다")
			}
			spec, err := Validate(r.Spec)
			if err != nil {
				cancel()
				return nil, err
			}
			count += len(spec.Addresses)
			if count > MaxTargets {
				cancel()
				return nil, errors.New("저장된 대상이 1,000개를 초과합니다")
			}
			j := newJob(r.ID, spec, r.CreatedAt)
			j.EndsAt = r.EndsAt
			j.Status = r.Status
			j.FinishedAt = r.FinishedAt
			if j.Status == "running" {
				j.Status = "interrupted"
				now := time.Now()
				j.FinishedAt = &now
			}
			e.jobs[j.ID] = j
		}
		if err = e.persistLocked(); err != nil {
			cancel()
			return nil, err
		}
	}
	e.wg.Add(1)
	go e.run()
	return e, nil
}
func newJob(id string, s Spec, at time.Time) *Job {
	j := &Job{ID: id, Spec: s, Status: "running", CreatedAt: at, EndsAt: at.Add(time.Duration(s.Duration) * time.Second), Targets: []*Target{}}
	for _, a := range s.Addresses {
		kind := "icmp"
		if strings.Contains(a, "://") {
			kind = "http"
		}
		j.Targets = append(j.Targets, &Target{Address: a, Kind: kind, History: []Result{}})
	}
	return j
}
func (e *Engine) Create(spec Spec) (string, error) {
	s, err := Validate(spec)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx.Err() != nil {
		return "", errors.New("서버가 종료 중입니다")
	}
	if len(e.jobs) >= 100 {
		return "", errors.New("작업은 최대 100개예요. 이전 작업을 삭제해 주세요")
	}
	n, rate := len(s.Addresses), float64(len(s.Addresses))/float64(s.Interval)
	for _, j := range e.jobs {
		n += len(j.Targets)
		if j.Status == "running" {
			rate += float64(len(j.Targets)) / float64(j.Spec.Interval)
		}
	}
	if n > MaxTargets {
		return "", errors.New("전체 대상은 1,000개 이하여야 해요. 이전 작업을 삭제해 주세요")
	}
	if rate > MaxRate+1e-9 {
		return "", errors.New("실행 중인 작업을 합쳐 초당 20회를 초과해요")
	}
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	j := newJob(hex.EncodeToString(id[:]), s, time.Now())
	j.ctx, j.cancel = context.WithDeadline(e.ctx, j.EndsAt)
	e.jobs[j.ID] = j
	if err := e.persistLocked(); err != nil {
		delete(e.jobs, j.ID)
		j.cancel()
		return "", fmt.Errorf("%w: %v", ErrStorage, err)
	}
	// Spread first probes over one interval, capped by the job's duration.
	window := time.Duration(s.Interval) * time.Second
	if d := time.Duration(s.Duration) * time.Second; window > d {
		window = d
	}
	for i, t := range j.Targets {
		heap.Push(&e.queue, &task{at: j.CreatedAt.Add(time.Duration(i) * window / time.Duration(len(j.Targets))), job: j, target: t})
	}
	return j.ID, nil
}
func (e *Engine) Stop(id string, remove bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	j, ok := e.jobs[id]
	if !ok {
		return os.ErrNotExist
	}
	oldStatus, oldFinished := j.Status, j.FinishedAt
	if remove {
		delete(e.jobs, id)
	} else if j.Status == "running" {
		j.Status = "stopped"
		now := time.Now()
		j.FinishedAt = &now
	}
	if err := e.persistLocked(); err != nil {
		e.jobs[id] = j
		j.Status = oldStatus
		j.FinishedAt = oldFinished
		return err
	}
	if j.cancel != nil {
		j.cancel()
	}
	e.pruneLocked()
	return nil
}
func (e *Engine) pruneLocked() {
	q := schedule{}
	for _, t := range e.queue {
		if t.job.Status == "running" && e.jobs[t.job.ID] == t.job {
			q = append(q, t)
		}
	}
	e.queue = q
	heap.Init(&e.queue)
}
func (e *Engine) JSON() []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	jobs := make([]*Job, 0, len(e.jobs))
	for _, j := range e.jobs {
		// Keep polling payload bounded: full history is fetched only on demand.
		copyJob := *j
		copyJob.Targets = make([]*Target, 0, len(j.Targets))
		for _, target := range j.Targets {
			copyTarget := *target
			copyTarget.History = []Result{}
			if len(target.History) > 0 {
				copyTarget.History = target.History[len(target.History)-1:]
			}
			copyJob.Targets = append(copyJob.Targets, &copyTarget)
		}
		jobs = append(jobs, &copyJob)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.After(jobs[j].CreatedAt) })
	recent := []Event{}
	for i := len(e.recent) - 1; i >= 0; i-- {
		if e.jobs[e.recent[i].Job] != nil {
			recent = append(recent, e.recent[i])
		}
	}
	b, _ := json.Marshal(map[string]any{"jobs": jobs, "recent": recent, "limits": map[string]any{"max_targets": MaxTargets, "max_rate": MaxRate, "concurrency": MaxConcurrent, "history": HistorySize}, "server_time": time.Now()})
	return b
}
func (e *Engine) JobJSON(id string) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	j := e.jobs[id]
	if j == nil {
		return nil, os.ErrNotExist
	}
	return json.Marshal(j)
}
func (e *Engine) persistLocked() error {
	rows := []saved{}
	for _, j := range e.jobs {
		rows = append(rows, saved{j.ID, j.Spec, j.CreatedAt, j.EndsAt, j.FinishedAt, j.Status})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	b, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(e.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(e.path), ".pingu-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), e.path)
}
func (e *Engine) run() {
	defer e.wg.Done()
	ticker := time.NewTicker(time.Second / 20)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case now := <-ticker.C:
			e.tick(now)
		}
	}
}
func (e *Engine) tick(now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	changed := false
	for id, j := range e.jobs {
		if j.Status == "running" && !now.Before(j.EndsAt) {
			j.Status = "completed"
			end := j.EndsAt
			j.FinishedAt = &end
			j.cancel()
			changed = true
		}
		if j.Status != "running" && j.FinishedAt != nil && now.Sub(*j.FinishedAt) > 24*time.Hour {
			delete(e.jobs, id)
			changed = true
		}
	}
	if changed {
		e.pruneLocked()
	}
	if changed || (!e.retryAfter.IsZero() && !now.Before(e.retryAfter)) {
		if err := e.persistLocked(); err != nil {
			log.Printf("작업 상태 저장 실패 (1분 후 재시도): %v", err)
			e.retryAfter = now.Add(time.Minute)
		} else {
			e.retryAfter = time.Time{}
		}
	}
	if len(e.queue) == 0 || e.queue[0].at.After(now) {
		return
	}
	t := heap.Pop(&e.queue).(*task)
	j, target := t.job, t.target
	if j.Status != "running" || e.jobs[j.ID] != j {
		return
	}
	interval := time.Duration(j.Spec.Interval) * time.Second
	missed := int(now.Sub(t.at) / interval)
	target.Skipped += missed
	t.at = t.at.Add(time.Duration(missed+1) * interval)
	if t.at.Before(j.EndsAt) {
		heap.Push(&e.queue, t)
	}
	if target.busy {
		target.Skipped++
		return
	}
	select {
	case e.slots <- struct{}{}:
	default:
		target.Skipped++
		return
	}
	target.busy = true
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() { <-e.slots }()
		ctx, cancel := context.WithTimeout(j.ctx, time.Duration(j.Spec.Timeout)*time.Second)
		defer cancel()
		r := e.probe(ctx, target.Kind, target.Address)
		r.At = time.Now()
		e.mu.Lock()
		defer e.mu.Unlock()
		target.busy = false
		if j.ctx.Err() != nil || j.Status != "running" || e.jobs[j.ID] != j {
			return
		}
		target.Count++
		if r.OK {
			target.Success++
			target.TotalMS += r.Latency
			if target.Success == 1 {
				target.MinMS = r.Latency
			} else {
				target.MinMS = math.Min(target.MinMS, r.Latency)
			}
			target.MaxMS = math.Max(target.MaxMS, r.Latency)
		}
		if len(target.History) == HistorySize {
			copy(target.History, target.History[1:])
			target.History = target.History[:HistorySize-1]
		}
		target.History = append(target.History, r)
		if len(e.recent) == HistorySize {
			copy(e.recent, e.recent[1:])
			e.recent = e.recent[:HistorySize-1]
		}
		e.recent = append(e.recent, Event{Result: r, Address: target.Address, Kind: target.Kind, Job: j.ID})
	}()
}
func (e *Engine) Close() {
	e.cancel()
	e.mu.Lock()
	for _, j := range e.jobs {
		if j.cancel != nil {
			j.cancel()
		}
	}
	e.mu.Unlock()
	e.wg.Wait()
}
