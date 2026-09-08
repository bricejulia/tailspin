package gcplog

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
)

func TestSlidingWindowLimiterGrantsUpToLimitImmediately(t *testing.T) {
	l := newSlidingWindowLimiter(3, 200*time.Millisecond)

	start := time.Now()
	for i := range 3 {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatalf("Wait() #%d = %v, want nil", i, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Errorf("3 grants under a limit of 3 took %v, want them to return immediately (a quiet limiter should burst)", elapsed)
	}
}

func TestSlidingWindowLimiterBlocksNPlus1UntilWindowElapses(t *testing.T) {
	window := 100 * time.Millisecond
	l := newSlidingWindowLimiter(1, window)

	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first Wait() = %v, want nil", err)
	}

	start := time.Now()
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("second Wait() = %v, want nil", err)
	}
	elapsed := time.Since(start)
	if elapsed < window-10*time.Millisecond {
		t.Errorf("second Wait() returned after %v, want >= ~%v (the (limit+1)th request must wait out the full window, not a token-bucket's window/limit)", elapsed, window)
	}
	if elapsed > 2*window {
		t.Errorf("second Wait() returned after %v, want well under 2x the window (%v)", elapsed, 2*window)
	}
}

// TestSlidingWindowLimiterNeverExceedsLimitUnderConcurrentLoad is the
// property this whole type exists for: unlike a token bucket with
// burst==limit (which can grant up to 2x limit in a rolling window once a
// backlog queues up — see the doc comment on slidingWindowLimiter), this
// must never grant more than limit requests within any trailing
// window-long interval, no matter how many goroutines are hammering it
// concurrently.
func TestSlidingWindowLimiterNeverExceedsLimitUnderConcurrentLoad(t *testing.T) {
	const limit = 5
	window := 100 * time.Millisecond
	l := newSlidingWindowLimiter(limit, window)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var (
		mu     sync.Mutex
		grants []time.Time
	)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for {
				if err := l.Wait(ctx); err != nil {
					return // ctx deadline hit — expected once the test window closes
				}
				mu.Lock()
				grants = append(grants, time.Now())
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	sort.Slice(grants, func(i, j int) bool { return grants[i].Before(grants[j]) })
	for i, t0 := range grants {
		count := 0
		for _, g := range grants[i:] {
			if g.Before(t0.Add(window)) {
				count++
			} else {
				break
			}
		}
		if count > limit {
			t.Fatalf("found %d grants within [%v, %v) starting at grants[%d] — want <= %d in any trailing window", count, t0, t0.Add(window), i, limit)
		}
	}
	if len(grants) == 0 {
		t.Fatal("no grants recorded at all — test setup is broken")
	}
}

func TestSlidingWindowLimiterContextCancellationDoesNotConsumeASlot(t *testing.T) {
	window := 200 * time.Millisecond
	l := newSlidingWindowLimiter(1, window)

	firstGrant := time.Now()
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first Wait() = %v, want nil", err)
	}

	shortCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := l.Wait(shortCtx)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait() with an already-consumed slot and a short ctx = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("cancelled Wait() took %v, want it to return promptly around the ctx's own 20ms timeout, not the limiter's window", elapsed)
	}

	// The cancelled call above must not have consumed the slot that opens
	// up when firstGrant's window elapses — a background-context Wait
	// should unblock right around when that slot frees, not later.
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("third Wait() = %v, want nil", err)
	}
	if since := time.Since(firstGrant); since < window-20*time.Millisecond {
		t.Errorf("third Wait() unblocked after only %v since the first grant, want close to the full window (%v) — the cancelled call must not have double-booked the freed slot", since, window)
	}
}

// stubInvoker returns a grpc.UnaryInvoker that counts calls and returns
// err (nil by default) — used to exercise rateLimitInterceptor with no
// real network/gRPC server: neither the interceptor nor this stub ever
// dereferences the *grpc.ClientConn a real call site would pass, so nil is
// fine.
func stubInvoker(callCount *int, err error) grpc.UnaryInvoker {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		*callCount++
		return err
	}
}

func TestRateLimitInterceptorSkipsInvokerWhenWaitFails(t *testing.T) {
	l := newSlidingWindowLimiter(1, time.Second)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("priming Wait() = %v, want nil", err)
	}
	interceptor := rateLimitInterceptor(l)

	var calls int
	shortCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := interceptor(shortCtx, "/some.Service/Method", nil, nil, nil, stubInvoker(&calls, nil))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("interceptor() = %v, want context.DeadlineExceeded", err)
	}
	if calls != 0 {
		t.Errorf("invoker was called %d times, want 0 — Wait() failing must short-circuit before invoking the RPC", calls)
	}
}

func TestRateLimitInterceptorInvokesAndPropagatesInvokerError(t *testing.T) {
	l := newSlidingWindowLimiter(10, time.Second) // large enough to never block
	interceptor := rateLimitInterceptor(l)

	wantErr := errors.New("boom")
	var calls int
	err := interceptor(context.Background(), "/some.Service/Method", nil, nil, nil, stubInvoker(&calls, wantErr))
	if !errors.Is(err, wantErr) {
		t.Errorf("interceptor() = %v, want %v", err, wantErr)
	}
	if calls != 1 {
		t.Errorf("invoker was called %d times, want exactly 1", calls)
	}
}

func TestRateLimitInterceptorPacesBackToBackCalls(t *testing.T) {
	const limit = 2
	window := 100 * time.Millisecond
	l := newSlidingWindowLimiter(limit, window)
	interceptor := rateLimitInterceptor(l)

	var calls int
	start := time.Now()
	for i := range 3 {
		if err := interceptor(context.Background(), "/some.Service/Method", nil, nil, nil, stubInvoker(&calls, nil)); err != nil {
			t.Fatalf("interceptor() call #%d = %v, want nil", i, err)
		}
	}
	elapsed := time.Since(start)
	if calls != 3 {
		t.Fatalf("invoker was called %d times, want 3", calls)
	}
	if elapsed < window-10*time.Millisecond {
		t.Errorf("3 calls through a limit-2 interceptor took %v, want the 3rd to wait out roughly the window (%v)", elapsed, window)
	}
}

func TestNewReadLimiterDialOptionConfiguresLimiter(t *testing.T) {
	limiter, opt := newReadLimiterDialOption(42, 30*time.Second)
	if limiter.limit != 42 {
		t.Errorf("limiter.limit = %d, want 42", limiter.limit)
	}
	if limiter.window != 30*time.Second {
		t.Errorf("limiter.window = %v, want 30s", limiter.window)
	}
	if opt == nil {
		t.Error("newReadLimiterDialOption returned a nil option.ClientOption")
	}
}
