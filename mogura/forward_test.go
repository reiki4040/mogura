package mogura

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// stubConn replaces the error of one method, so the tests can drive the paths
// a real connection only reaches while it is being torn down.
type stubConn struct {
	net.Conn

	closeErr error
	readErr  error
}

func (c *stubConn) Close() error {
	c.Conn.Close()

	if c.closeErr != nil {
		return c.closeErr
	}

	return nil
}

func (c *stubConn) Read(b []byte) (int, error) {
	if c.readErr != nil {
		return 0, c.readErr
	}

	return c.Conn.Read(b)
}

// collectErrs drains what forward reported. it waits a moment first, the copy
// goroutines can report after forward returned.
func collectErrs(errChan chan error) []error {
	time.Sleep(100 * time.Millisecond)

	errs := []error{}
	for {
		select {
		case err := <-errChan:
			errs = append(errs, err)
		default:
			return errs
		}
	}
}

func TestForwardReportsNothingWhenBothSidesFinish(t *testing.T) {
	// Arrange
	client, localConn := net.Pipe()
	sshConn, remote := net.Pipe()
	errChan := make(chan error, 8)

	go func() {
		client.Write([]byte("GET / HTTP/1.1\r\n\r\n"))
		io.ReadAll(remote)
	}()

	// both ends go away, which is how a client finishes with a connection.
	go func() {
		time.Sleep(50 * time.Millisecond)
		client.Close()
		remote.Close()
	}()

	// Act
	forward(context.Background(), localConn, sshConn, 5*time.Second, errChan)

	// Assert
	if errs := collectErrs(errChan); len(errs) != 0 {
		t.Errorf("want a finished transfer to report nothing, got %v", errs)
	}
}

func TestForwardReportsTimeout(t *testing.T) {
	// Arrange
	// nothing closes the connection, so only the timeout ends the forwarding.
	_, localConn := net.Pipe()
	sshConn, _ := net.Pipe()
	errChan := make(chan error, 8)

	// Act
	forward(context.Background(), localConn, sshConn, 50*time.Millisecond, errChan)

	// Assert
	errs := collectErrs(errChan)
	if len(errs) != 1 {
		t.Fatalf("want only the timeout reported, got %v", errs)
	}
	if !strings.Contains(errs[0].Error(), "forwarding timeout") {
		t.Errorf("want the timeout reported, got %v", errs[0])
	}
	if !strings.Contains(errs[0].Error(), "forwarding_timeout") {
		t.Errorf("want the setting to raise named in the message, got %v", errs[0])
	}
}

func TestForwardCloseErrors(t *testing.T) {
	cases := []struct {
		name       string
		closeErr   error
		wantReport bool
	}{
		{
			// mogura closes both sides itself, so the other close finds the
			// connection already gone.
			name:       "already closed is not a failure",
			closeErr:   net.ErrClosed,
			wantReport: false,
		},
		{
			// an ssh channel of a connection that is gone reports EOF.
			name:       "EOF is not a failure",
			closeErr:   io.EOF,
			wantReport: false,
		},
		{
			name:       "any other error is reported",
			closeErr:   errors.New("connection reset by peer"),
			wantReport: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Arrange
			client, rawLocal := net.Pipe()
			sshConn, remote := net.Pipe()
			localConn := &stubConn{Conn: rawLocal, closeErr: c.closeErr}
			errChan := make(chan error, 8)

			go func() {
				time.Sleep(20 * time.Millisecond)
				client.Close()
				remote.Close()
			}()

			// Act
			forward(context.Background(), localConn, sshConn, 5*time.Second, errChan)

			// Assert
			errs := collectErrs(errChan)
			if !c.wantReport {
				if len(errs) != 0 {
					t.Fatalf("want no report, got %v", errs)
				}
				return
			}

			if len(errs) != 1 {
				t.Fatalf("want the close failure reported, got %v", errs)
			}
			if !strings.Contains(errs[0].Error(), "failed close local conn") {
				t.Errorf("want the close failure reported, got %v", errs[0])
			}
		})
	}
}

func TestForwardCopyErrors(t *testing.T) {
	cases := []struct {
		name       string
		readErr    error
		wantReport bool
	}{
		{
			// the copy is still running when mogura closes the connection.
			name:       "reading a closed connection is not a failure",
			readErr:    net.ErrClosed,
			wantReport: false,
		},
		{
			name:       "any other error is reported",
			readErr:    errors.New("connection reset by peer"),
			wantReport: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Arrange
			_, rawLocal := net.Pipe()
			sshConn, remote := net.Pipe()
			localConn := &stubConn{Conn: rawLocal, readErr: c.readErr}
			errChan := make(chan error, 8)

			go func() {
				time.Sleep(20 * time.Millisecond)
				remote.Close()
			}()

			// Act
			forward(context.Background(), localConn, sshConn, 5*time.Second, errChan)

			// Assert
			errs := collectErrs(errChan)
			if !c.wantReport {
				if len(errs) != 0 {
					t.Fatalf("want no report, got %v", errs)
				}
				return
			}

			if len(errs) != 1 {
				t.Fatalf("want the transfer failure reported, got %v", errs)
			}
			if !strings.Contains(errs[0].Error(), "local -> remote transfer failed") {
				t.Errorf("want the direction of the failure reported, got %v", errs[0])
			}
		})
	}
}
