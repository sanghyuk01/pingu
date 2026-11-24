package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func spec() Spec {
	return Spec{Name: "test", Addresses: []string{"1.1.1.1"}, Interval: 1, Duration: 120, Timeout: 2}
}
func jobSnapshot(t *testing.T, e *Engine, id string) Job {
	t.Helper()
	b, err := e.JobJSON(id)
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err = json.Unmarshal(b, &j); err != nil {
		t.Fatal(err)
	}
	return j
}
func engine(t *testing.T, probe ProbeFunc) *Engine {
	t.Helper()
	e, err := NewEngine(filepath.Join(t.TempDir(), "jobs.json"), probe)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestHistoryBoundedAndStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := engine(t, func(context.Context, string, string) Result { return Result{OK: true, Latency: 4} })
		defer e.Close()
		id, err := e.Create(spec())
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(65 * time.Second)
		synctest.Wait()
		j := jobSnapshot(t, e, id)
		target := j.Targets[0]
		if len(target.History) != HistorySize || target.Count < 64 || target.Success != target.Count {
			t.Fatalf("unexpected history: count=%d success=%d history=%d", target.Count, target.Success, len(target.History))
		}
		if err = e.Stop(id, false); err != nil {
			t.Fatal(err)
		}
		before := jobSnapshot(t, e, id).Targets[0].Count
		time.Sleep(4 * time.Second)
		synctest.Wait()
		j = jobSnapshot(t, e, id)
		if j.Status != "stopped" || j.Targets[0].Count != before {
			t.Fatal("stopped job continued probing")
		}
	})
}
func TestCompletionCancelsPendingWithoutFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var canceled atomic.Bool
		e := engine(t, func(ctx context.Context, _, _ string) Result {
			<-ctx.Done()
			canceled.Store(true)
			return Result{Error: ctx.Err().Error()}
		})
		defer e.Close()
		s := spec()
		s.Duration = 1
		s.Timeout = 10
		id, err := e.Create(s)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		j := jobSnapshot(t, e, id)
		if j.Status != "completed" || !canceled.Load() || j.Targets[0].Count != 0 {
			t.Fatalf("completion: %+v", j)
		}
	})
}
func TestGlobalRateAndConcurrentLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var active, peak atomic.Int32
		e := engine(t, func(ctx context.Context, _, _ string) Result {
			v := active.Add(1)
			for {
				old := peak.Load()
				if v <= old || peak.CompareAndSwap(old, v) {
					break
				}
			}
			<-ctx.Done()
			active.Add(-1)
			return Result{}
		})
		defer e.Close()
		s := spec()
		s.Addresses = nil
		s.Interval = 4
		s.Timeout = 10
		for i := 0; i < 80; i++ {
			s.Addresses = append(s.Addresses, fmt.Sprintf("test-%d.example.com", i))
		}
		id, err := e.Create(s)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.Create(spec()); err == nil {
			t.Fatal("accepted global rate >20")
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		if peak.Load() != MaxConcurrent {
			t.Fatalf("peak concurrency = %d", peak.Load())
		}
		j := jobSnapshot(t, e, id)
		skipped := 0
		for _, target := range j.Targets {
			skipped += target.Skipped
		}
		if skipped == 0 {
			t.Fatal("expected skipped probes at capacity")
		}
	})
}
func TestRestartRetainsDefinitionsNotResults(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "jobs.json")
		probe := func(context.Context, string, string) Result { return Result{OK: true} }
		e, err := NewEngine(path, probe)
		if err != nil {
			t.Fatal(err)
		}
		id, err := e.Create(spec())
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		e.Close()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "history") {
			t.Fatal("persisted probe results")
		}
		restored, err := NewEngine(path, probe)
		if err != nil {
			t.Fatal(err)
		}
		defer restored.Close()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		j := jobSnapshot(t, restored, id)
		if j.Status != "interrupted" || j.Targets[0].Count != 0 || len(j.Targets[0].History) != 0 {
			t.Fatal("restart resumed work or retained results")
		}
	})
}
func TestValidation(t *testing.T) {
	for _, address := range []string{"file:///etc/passwd", "https://u:p@example.com", "https://example.com/#secret", "hello/world", "-bad.example", "https://example.com:99999"} {
		s := spec()
		s.Addresses = []string{address}
		if _, err := Validate(s); err == nil {
			t.Errorf("accepted %s", address)
		}
	}
	s := spec()
	s.Addresses = []string{"1.1.1.1", "1.1.1.1"}
	got, err := Validate(s)
	if err != nil || len(got.Addresses) != 1 {
		t.Fatal("deduplication failed")
	}
}
