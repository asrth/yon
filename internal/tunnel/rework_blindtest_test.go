package tunnel

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ultramcu/yon/internal/model"
)

// This file is a BLIND test author's encoding of two reworked-cell invariants.
// It is written purely from the spec (ADR 0001 single-dial invariant + the
// production keepalive contract), not from the Dev's fix. Local fakes use a
// distinct "rw" prefix so they never clash with the existing fakeConn /
// fakeDialer / bt2* / hk2* helpers already in the package.

// rwBlockingConn is a fake SSHConn handed out by rwBlockingDialer. Its only job
// in Test A is to be a distinct, identifiable connection (so callers can prove
// they all got the SAME one). DialContext returns a real (unconnected) pipe end
// so callers receive a non-nil net.Conn.
type rwBlockingConn struct {
	id     int
	closed int32
}

func (c *rwBlockingConn) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	client, _ := net.Pipe()
	return client, nil
}

func (c *rwBlockingConn) Keepalive() error { return nil }

func (c *rwBlockingConn) Close() error {
	atomic.AddInt32(&c.closed, 1)
	return nil
}

// rwBlockingDialer is an SSHDialer that (a) atomically counts invocations, (b)
// signals (via entered) the moment each call enters the dial, and (c) blocks
// inside the dial on the release channel until the test unblocks it. This lets
// the test drive all N goroutines into the cold-connect path BEFORE any dial
// completes — the precondition for exercising the concurrent-first-dial race.
type rwBlockingDialer struct {
	calls   int32         // total dial invocations (atomic)
	entered chan struct{} // one send per dial that has entered (and is about to block)
	release chan struct{} // closed by the test to let blocked dials proceed
	nextID  int32         // hands each returned conn a distinct id (atomic)
}

func newRWBlockingDialer(n int) *rwBlockingDialer {
	return &rwBlockingDialer{
		entered: make(chan struct{}, n),
		release: make(chan struct{}),
	}
}

func (d *rwBlockingDialer) dial(ctx context.Context, jh model.JumpHost, hk ssh.HostKeyCallback) (SSHConn, error) {
	atomic.AddInt32(&d.calls, 1)
	// Announce arrival, then block until the test releases us. Because every
	// caller blocks here, the test can guarantee they are all simultaneously in
	// the cold state before the first dial returns.
	d.entered <- struct{}{}
	<-d.release
	id := int(atomic.AddInt32(&d.nextID, 1))
	return &rwBlockingConn{id: id}, nil
}

func (d *rwBlockingDialer) callCount() int { return int(atomic.LoadInt32(&d.calls)) }

// TestEnsure_ConcurrentFirstDial_DialsOnce encodes the ADR 0001 invariant: when
// many requests fire at once on a COLD tunnel, the first opens the SSH
// connection and the rest reuse it — so the dialer is invoked EXACTLY ONCE and
// every caller receives the same winning connection.
//
// Fail-before: the current ensure() releases mu around the dial without an
// in-flight guard, so all N goroutines see Disconnected, all enter dial, and
// the count is N (~50). This test will then deadlock/timeout because the dialer
// only signals on the first arrival before the test releases; we instead pump
// arrivals continuously so the broken code surfaces a count of N rather than
// hanging.
func TestEnsure_ConcurrentFirstDial_DialsOnce(t *testing.T) {
	const n = 50

	d := newRWBlockingDialer(n)
	m := New(WithSSHDialer(d.dial))

	jh := testJumpHost()
	_, release, err := m.Acquire(jh)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	dialer := m.DialContext(jh)

	type result struct {
		conn net.Conn
		err  error
	}
	results := make(chan result, n)

	var start sync.WaitGroup
	start.Add(1)
	var launched sync.WaitGroup
	launched.Add(n)

	for i := 0; i < n; i++ {
		go func() {
			launched.Done()
			start.Wait() // all goroutines released together
			conn, err := dialer(context.Background(), "tcp", "target:1")
			results <- result{conn: conn, err: err}
		}()
	}

	launched.Wait() // every goroutine exists and is parked on start
	start.Done()    // fire them all at once

	// Drain "entered" signals continuously so a broken (N-dial) implementation
	// never blocks on the unbuffered side, while a correct (1-dial)
	// implementation simply produces a single arrival. We release the very first
	// arrival as soon as we see it; that is enough for the correct code to let
	// the winner finish and the rest reuse the result.
	go func() {
		<-d.entered      // wait for at least one dial to enter
		close(d.release) // let every (current and future) blocked dial proceed
	}()

	// Collect all N caller results with a generous timeout.
	got := make([]result, 0, n)
	deadline := time.After(5 * time.Second)
	for len(got) < n {
		select {
		case r := <-results:
			got = append(got, r)
		case <-deadline:
			t.Fatalf("timed out: only %d/%d callers returned (dials so far=%d)",
				len(got), n, d.callCount())
		}
	}

	// Invariant 1: the dialer was invoked exactly once.
	if c := d.callCount(); c != 1 {
		t.Fatalf("ADR 0001 violated: cold concurrent first-dial opened the bastion %d times, want exactly 1", c)
	}

	// Invariant 2: every caller got a non-nil conn and nil error.
	for i, r := range got {
		if r.err != nil {
			t.Fatalf("caller %d got error %v, want nil", i, r.err)
		}
		if r.conn == nil {
			t.Fatalf("caller %d got nil conn, want a live conn", i)
		}
	}

	// Invariant 3: all callers share the SAME underlying winning connection.
	// The fake's DialContext returns one pipe end per call THROUGH the conn, so
	// we can't compare net.Conn identity directly; instead we assert that the
	// single dial produced a single SSHConn id and nothing was discarded as a
	// race-loser (which only happens when >1 dial occurred). With exactly one
	// dial, nextID==1 and no rwBlockingConn was ever Close()d as a loser.
	if id := atomic.LoadInt32(&d.nextID); id != 1 {
		t.Fatalf("expected exactly one SSHConn produced (winner), got %d distinct conns", id)
	}
}

// rwKeepaliveConn is a fake SSHConn whose Keepalive() succeeds a fixed number of
// times and then returns an error deterministically — driving the production
// keepalive loop's ticker→probe→markDropped path.
type rwKeepaliveConn struct {
	mu       sync.Mutex
	probes   int
	okBefore int // succeed this many probes, then error
	closed   bool
}

func (c *rwKeepaliveConn) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	client, _ := net.Pipe()
	return client, nil
}

func (c *rwKeepaliveConn) Keepalive() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.probes++
	if c.probes > c.okBefore {
		return errors.New("simulated keepalive failure")
	}
	return nil
}

func (c *rwKeepaliveConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// TestKeepalive_RealLoop_MarksDropped encodes the production keepalive contract:
// the Manager's keepalive loop runs on an injectable interval and, when
// SSHConn.Keepalive() returns an error, transitions the Tunnel to State == Error
// via the real ticker→probe→markDropped path (NOT by the test calling
// markDropped directly).
//
// Contract under test (built to these exact names by the Dev):
//   - withKeepaliveInterval(d time.Duration) Option   // mirrors withClock
//
// Fail-before: withKeepaliveInterval does not exist yet, so this test won't
// compile against the current tree — the intended fail-before for Test B. Once
// the option lands and the loop honours it, a tiny interval makes the loop
// detect the drop and flip the tunnel to Error within the poll window.
func TestKeepalive_RealLoop_MarksDropped(t *testing.T) {
	// Succeed once, then fail — proving the loop actually ticks repeatedly and
	// reacts to the error rather than being a one-shot.
	conn := &rwKeepaliveConn{okBefore: 1}

	dialer := func(ctx context.Context, jh model.JumpHost, hk ssh.HostKeyCallback) (SSHConn, error) {
		return conn, nil
	}

	m := New(
		WithSSHDialer(dialer),
		withKeepaliveInterval(5*time.Millisecond),
	)

	jh := testJumpHost()
	_, release, err := m.Acquire(jh)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	// Bring the tunnel up; this must start the keepalive loop.
	if err := m.Connect(context.Background(), jh); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	id := identity(jh)

	// Poll Status until the keepalive loop flips the tunnel to Error.
	var last TunnelStatus
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			t.Fatalf("keepalive loop never marked the tunnel dropped; last state=%v err=%q",
				last.State, last.Err)
		case <-tick.C:
			for _, s := range m.Status() {
				if s.ID == id {
					last = s
				}
			}
			if last.State == Error {
				if last.Err == "" {
					t.Fatalf("tunnel reached Error but Err is empty; want a drop message")
				}
				if !strings.Contains(strings.ToLower(last.Err), "drop") {
					t.Fatalf("Error message %q does not mention the drop", last.Err)
				}
				return // contract satisfied
			}
		}
	}
}
