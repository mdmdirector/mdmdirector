package db

import (
	"errors"
	"io"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/jackc/pgconn"
)

func TestIsStaleConnection(t *testing.T) {
	reset := &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"reset by peer", reset, true},
		{"unexpected EOF", io.ErrUnexpectedEOF, true},
		{"postgres error", &pgconn.PgError{Code: "23505"}, false},
		{"other", errors.New("boom"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsStaleConnection(tt.err); got != tt.want {
				t.Fatalf("IsStaleConnection(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestRetryRead(t *testing.T) {
	t.Run("retries once on a stale connection", func(t *testing.T) {
		calls := 0
		err := RetryRead(func() error {
			calls++
			if calls == 1 {
				return io.ErrUnexpectedEOF
			}
			return nil
		})
		if err != nil || calls != 2 {
			t.Fatalf("err=%v calls=%d, want nil and 2", err, calls)
		}
	})

	t.Run("gives up after the second stale failure", func(t *testing.T) {
		calls := 0
		err := RetryRead(func() error {
			calls++
			return io.ErrUnexpectedEOF
		})
		if !errors.Is(err, io.ErrUnexpectedEOF) || calls != 2 {
			t.Fatalf("err=%v calls=%d, want unexpected EOF and 2", err, calls)
		}
	})

	t.Run("does not retry other errors", func(t *testing.T) {
		calls := 0
		want := &pgconn.PgError{Code: "42P01"}
		err := RetryRead(func() error {
			calls++
			return want
		})
		if !errors.Is(err, want) || calls != 1 {
			t.Fatalf("err=%v calls=%d, want pg error and 1", err, calls)
		}
	})
}
