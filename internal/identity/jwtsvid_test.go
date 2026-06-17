package identity

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeFetcher is a programmable jwtFetcher; fetch optionally blocks on gate.
type fakeFetcher struct {
	mu    sync.Mutex
	calls int
	tok   string
	exp   time.Time
	err   error
	gate  chan struct{}
}

func (f *fakeFetcher) fetch(_ context.Context, _ string) (string, time.Time, error) {
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.tok, f.exp, f.err
}

func (f *fakeFetcher) close() error { return nil }

func (f *fakeFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeFetcher) set(tok string, exp time.Time, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tok, f.exp, f.err = tok, exp, err
}

// clock is a thread-safe injectable clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTestProvider(f jwtFetcher, now func() time.Time) *JWTSVIDProvider {
	p := newJWTSVIDProviderWith(f, "broker.example", 60*time.Second)
	p.now = now
	return p
}

var base = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestJWTSVIDCacheHit(t *testing.T) {
	clk := &clock{t: base}
	f := &fakeFetcher{tok: "tok1", exp: base.Add(5 * time.Minute)}
	p := newTestProvider(f, clk.now)

	if tok, err := p.Token(context.Background()); err != nil || tok != "tok1" {
		t.Fatalf("first Token = %q, %v", tok, err)
	}
	clk.advance(30 * time.Second) // still before refreshAt (exp-60s)
	if tok, err := p.Token(context.Background()); err != nil || tok != "tok1" {
		t.Fatalf("second Token = %q, %v", tok, err)
	}
	if f.callCount() != 1 {
		t.Errorf("fetch calls = %d, want 1 (cache hit)", f.callCount())
	}
}

func TestJWTSVIDProactiveRefresh(t *testing.T) {
	clk := &clock{t: base}
	f := &fakeFetcher{tok: "tok1", exp: base.Add(5 * time.Minute)}
	p := newTestProvider(f, clk.now)

	if _, err := p.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.set("tok2", clk.now().Add(5*time.Minute), nil)
	clk.advance(4*time.Minute + time.Second) // past refreshAt (exp-60s), before exp

	tok, err := p.Token(context.Background())
	if err != nil || tok != "tok2" {
		t.Fatalf("Token = %q, %v, want tok2", tok, err)
	}
	if f.callCount() != 2 {
		t.Errorf("fetch calls = %d, want 2", f.callCount())
	}
}

func TestJWTSVIDServeStaleOnError(t *testing.T) {
	clk := &clock{t: base}
	f := &fakeFetcher{tok: "tok1", exp: base.Add(5 * time.Minute)}
	p := newTestProvider(f, clk.now)

	if _, err := p.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.set("tok1", base.Add(5*time.Minute), errors.New("agent down"))
	clk.advance(4*time.Minute + 30*time.Second) // refresh window, still before exp

	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("expected stale token served, got error %v", err)
	}
	if tok != "tok1" {
		t.Errorf("Token = %q, want stale tok1", tok)
	}
}

func TestJWTSVIDHardFailWhenColdAndErroring(t *testing.T) {
	clk := &clock{t: base}
	f := &fakeFetcher{err: errors.New("agent down")}
	p := newTestProvider(f, clk.now)

	if _, err := p.Token(context.Background()); err == nil {
		t.Error("want error when cache is cold and fetch fails")
	}
}

func TestJWTSVIDHardFailWhenExpiredAndErroring(t *testing.T) {
	clk := &clock{t: base}
	f := &fakeFetcher{tok: "tok1", exp: base.Add(5 * time.Minute)}
	p := newTestProvider(f, clk.now)
	if _, err := p.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.set("tok1", base.Add(5*time.Minute), errors.New("agent down"))
	clk.advance(6 * time.Minute) // past exp: stale token no longer valid

	if _, err := p.Token(context.Background()); err == nil {
		t.Error("want error when cached token has expired and fetch fails")
	}
}

func TestJWTSVIDConcurrencyCollapse(t *testing.T) {
	clk := &clock{t: base}
	gate := make(chan struct{})
	f := &fakeFetcher{tok: "tok1", exp: base.Add(5 * time.Minute), gate: gate}
	p := newTestProvider(f, clk.now)

	const n = 25
	var wg sync.WaitGroup
	toks := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			toks[i], errs[i] = p.Token(context.Background())
		}(i)
	}
	time.Sleep(50 * time.Millisecond) // let all goroutines block on the in-flight fetch
	close(gate)
	wg.Wait()

	if f.callCount() != 1 {
		t.Errorf("fetch calls = %d, want 1 (collapsed)", f.callCount())
	}
	for i := 0; i < n; i++ {
		if errs[i] != nil || toks[i] != "tok1" {
			t.Errorf("goroutine %d: %q, %v", i, toks[i], errs[i])
		}
	}
}
