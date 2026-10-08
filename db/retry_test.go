package db

import (
	"errors"
	"io"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/jackc/pgconn"
	pkgerrors "github.com/pkg/errors"
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
		{"wrapped by pkg/errors", pkgerrors.Wrap(io.ErrUnexpectedEOF, "reset device"), true},
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
