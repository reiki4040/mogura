package mogura

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"
)

// newTestMogura builds a Mogura that holds a real ssh client and a real local
// listener, so the tests exercise the same state the tunnel works with.
func newTestMogura(t *testing.T) *Mogura {
	t.Helper()

	m := &Mogura{
		Config: MoguraConfig{
			Name:                  "test",
			BastionHostPort:       startTestSSHServer(t, newTestED25519Signer(t)),
			Username:              "mogura",
			KeyPath:               writeTestPrivateKey(t),
			InsecureIgnoreHostKey: true,
			// port 0 lets the kernel pick a free port for the test.
			LocalBindPort:    "127.0.0.1:0",
			ForwardingTarget: Target{Target: "10.0.0.5", TargetPort: 80},
		},
	}
	m.localDoneChan = make(chan struct{})
	m.remoteDoneChan = make(chan struct{})

	if err := m.ConnectSSH(); err != nil {
		t.Fatalf("failed connect to the test bastion: %v", err)
	}
	if err := m.Listen(); err != nil {
		t.Fatalf("failed listen: %v", err)
	}

	return m
}

func TestCloseIsIdempotent(t *testing.T) {
	// Arrange
	m := newTestMogura(t)

	// Act
	if err := m.Close(); err != nil {
		t.Fatalf("first close failed: %v", err)
	}
	err := m.Close()

	// Assert
	if err != nil {
		t.Errorf("want the second close to do nothing, got error: %v", err)
	}
}

// TestCloseIsIdempotentAfterCloseError covers the case the local listener
// reports an error while closing. the close must still not run twice.
func TestCloseIsIdempotentAfterCloseError(t *testing.T) {
	// Arrange
	m := newTestMogura(t)

	// close the listener behind mogura, so that its own close reports an error.
	if err := m.localListener.Close(); err != nil {
		t.Fatalf("failed close the listener: %v", err)
	}

	// Act
	m.Close()
	m.Close()

	// Assert
	// reaching this line is the assertion: the second close must not panic.
}

func TestCloseFromSeveralGoroutines(t *testing.T) {
	// Arrange
	m := newTestMogura(t)

	// Act
	// the accept loop closes mogura when forwarding is prohibited, while the
	// signal handler closes it on Ctrl+C. both can run at the same time.
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			m.Close()
		})
	}
	wg.Wait()

	// Assert
	// reaching this line is the assertion: no close of closed channel panic.
}

// TestStateAccessIsRaceFree closes mogura while the accept loop and the
// resolve cycle are reading its state, which is what happens on Ctrl+C.
func TestStateAccessIsRaceFree(t *testing.T) {
	// Arrange
	m := newTestMogura(t)

	// Act
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 50 {
				// the accept loop reads the client and the target address.
				if client, remote, err := m.connection(); err == nil && client == nil {
					t.Errorf("want a client with %q, got nil", remote)
				}
				if _, err := m.listener(); err != nil && !errors.Is(err, errClosed) {
					t.Errorf("want the listener or errClosed, got %v", err)
				}
				// the resolve cycle writes the target address.
				m.setDetectedRemote("10.0.0." + strconv.Itoa(i) + ":" + strconv.Itoa(j))
			}
		})
	}

	wg.Go(func() {
		m.Close()
	})
	wg.Wait()

	// Assert
	// the race detector is the assertion. after the close every accessor must
	// report that mogura is closed instead of handing out a nil connection.
	if _, _, err := m.connection(); !errors.Is(err, errClosed) {
		t.Errorf("want errClosed after close, got %v", err)
	}
	if _, err := m.listener(); !errors.Is(err, errClosed) {
		t.Errorf("want errClosed after close, got %v", err)
	}
}

func TestConnectSSHDoesNotReviveAfterClose(t *testing.T) {
	// Arrange
	m := newTestMogura(t)
	if err := m.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// Act
	err := m.ConnectSSH()

	// Assert
	if !errors.Is(err, errClosed) {
		t.Fatalf("want errClosed, got %v", err)
	}
	if _, _, connErr := m.connection(); !errors.Is(connErr, errClosed) {
		t.Error("want mogura to stay closed, got a usable connection")
	}
}

// TestGoResolveCycleStopsOnClose covers the cycle outliving mogura: it used to
// keep ticking after the close, and a failed resolve made it dial the bastion
// again, which brought the connection back up after shutdown.
func TestGoResolveCycleStopsOnClose(t *testing.T) {
	// Arrange
	m := newTestMogura(t)
	// the target is a plain host and port, so resolving needs no dns server.
	errChan := m.GoResolveCycle(10 * time.Millisecond)

	// let the cycle run a few times before closing.
	time.Sleep(50 * time.Millisecond)

	// Act
	if err := m.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// Assert
	// the cycle closes its error channel when it stops.
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-errChan:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("want the resolve cycle to stop after close, it is still running")
		}
	}
}
