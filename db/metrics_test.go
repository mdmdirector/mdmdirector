package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgconn"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/mdmdirector/mdmdirector/director/metrics"
)

func TestDBResult(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"no error", nil, metrics.DBResultSuccess},
		{"no rows", gorm.ErrRecordNotFound, metrics.DBResultNotFound},
		{
			"postgres rejected the statement",
			&pgconn.PgError{Code: "23505", Message: "duplicate key value"},
			metrics.DBResultPostgresError,
		},
		{"caller gave up", context.Canceled, metrics.DBResultCanceled},
		{"ran out of time", context.DeadlineExceeded, metrics.DBResultTimeout},
		{"socket deadline", os.ErrDeadlineExceeded, metrics.DBResultTimeout},
		// The exact error pgproto3 produces when the far end closes the socket,
		// which is the failure this instrumentation exists to make visible.
		{"closed socket", io.ErrUnexpectedEOF, metrics.DBResultStaleConnection},
		{"pool rejected the connection", driver.ErrBadConn, metrics.DBResultStaleConnection},
		{"connection reset", &net.OpError{Err: syscall.ECONNRESET}, metrics.DBResultStaleConnection},
		{"broken pipe", &net.OpError{Err: syscall.EPIPE}, metrics.DBResultStaleConnection},
		{"unknown failure", errors.New("something else"), metrics.DBResultOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dbResult(tt.err); got != tt.want {
				t.Errorf("dbResult(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// Errors reach the classifier wrapped by gorm and by pgconn, so identity checks
// have to survive the layers rather than only matching a bare sentinel.
func TestDBResultSeesThroughWrapping(t *testing.T) {
	wrapped := fmt.Errorf("running query: %w", fmt.Errorf("receive message failed: %w", io.ErrUnexpectedEOF))

	if got := dbResult(wrapped); got != metrics.DBResultStaleConnection {
		t.Errorf("dbResult(wrapped EOF) = %q, want %q", got, metrics.DBResultStaleConnection)
	}
}

// A dead connection has to show up on the counter, through real gorm callbacks,
// or the alert built on it will stay silent during the next incident.
//
// Note the operation label: Raw().Scan() runs through gorm's Row processor, not
// its Raw one, which is only visible by watching what the callbacks report.
func TestMetricsCallbacksCountStaleConnection(t *testing.T) {
	mockDB, spy, err := sqlmock.New()
	if err != nil {
		t.Fatalf("creating mock: %v", err)
	}
	defer mockDB.Close()

	gdb, err := gorm.Open(
		postgres.New(postgres.Config{Conn: mockDB}),
		&gorm.Config{SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatalf("opening gorm: %v", err)
	}

	if err := registerMetricsCallbacks(gdb); err != nil {
		t.Fatalf("registering callbacks: %v", err)
	}

	before := testutil.ToFloat64(metrics.DBOperations("row", metrics.DBResultStaleConnection))

	spy.ExpectQuery(`.*`).WillReturnError(io.ErrUnexpectedEOF)

	var count int64
	_ = gdb.Raw("SELECT count(*) FROM devices").Scan(&count).Error

	after := testutil.ToFloat64(metrics.DBOperations("row", metrics.DBResultStaleConnection))

	if after != before+1 {
		t.Errorf("stale_connection count went from %v to %v, wanted one increment", before, after)
	}
}

type metricsTestRow struct {
	ID int64
}

// A healthy query must not land in an error bucket, or the alert is noise. This
// one goes through the Query processor, which is what most call sites use.
func TestMetricsCallbacksCountSuccess(t *testing.T) {
	mockDB, spy, err := sqlmock.New()
	if err != nil {
		t.Fatalf("creating mock: %v", err)
	}
	defer mockDB.Close()

	gdb, err := gorm.Open(
		postgres.New(postgres.Config{Conn: mockDB}),
		&gorm.Config{SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatalf("opening gorm: %v", err)
	}

	if err := registerMetricsCallbacks(gdb); err != nil {
		t.Fatalf("registering callbacks: %v", err)
	}

	before := testutil.ToFloat64(metrics.DBOperations("query", metrics.DBResultSuccess))

	spy.ExpectQuery(`.*`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(3)))

	var rows []metricsTestRow
	if err := gdb.Find(&rows).Error; err != nil {
		t.Fatalf("query failed: %v", err)
	}

	after := testutil.ToFloat64(metrics.DBOperations("query", metrics.DBResultSuccess))

	if after != before+1 {
		t.Errorf("success count went from %v to %v, wanted one increment", before, after)
	}
}
