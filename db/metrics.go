package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"os"
	"syscall"

	"github.com/jackc/pgconn"
	"gorm.io/gorm"

	"github.com/mdmdirector/mdmdirector/director/metrics"
)

// registerMetricsCallbacks counts the outcome of every gorm operation.
//
// Most database errors in mdmdirector are logged as a bare message and nothing
// else, which makes them impossible to alert on and impossible to tell apart
// from unrelated failures with the same wording. Counting them here, classified
// by cause, gives one place to watch regardless of which call site failed.
//
// The callbacks are ordered "*" so they run last, after the transaction gorm
// wraps writes in has been committed or rolled back, and therefore see the
// error the caller will see.
func registerMetricsCallbacks(gdb *gorm.DB) error {
	c := gdb.Callback()

	if err := c.Create().After("*").Register("metrics:after_create", metricsAfter("create")); err != nil {
		return err
	}
	if err := c.Query().After("*").Register("metrics:after_query", metricsAfter("query")); err != nil {
		return err
	}
	if err := c.Update().After("*").Register("metrics:after_update", metricsAfter("update")); err != nil {
		return err
	}
	if err := c.Delete().After("*").Register("metrics:after_delete", metricsAfter("delete")); err != nil {
		return err
	}
	if err := c.Row().After("*").Register("metrics:after_row", metricsAfter("row")); err != nil {
		return err
	}
	if err := c.Raw().After("*").Register("metrics:after_raw", metricsAfter("raw")); err != nil {
		return err
	}

	return nil
}

func metricsAfter(operation string) func(*gorm.DB) {
	return func(tx *gorm.DB) {
		metrics.DBOperations(operation, dbResult(tx.Error)).Inc()
	}
}

// dbResult classifies a database error so the counter can separate a broken
// connection from a rejected statement. The checks are on error identity rather
// than on message text.
func dbResult(err error) string {
	switch {
	case err == nil:
		return metrics.DBResultSuccess

	case errors.Is(err, gorm.ErrRecordNotFound):
		return metrics.DBResultNotFound

	case isPostgresError(err):
		return metrics.DBResultPostgresError

	case errors.Is(err, context.Canceled):
		return metrics.DBResultCanceled

	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, os.ErrDeadlineExceeded),
		pgconn.Timeout(err):
		return metrics.DBResultTimeout

	case errors.Is(err, driver.ErrBadConn),
		errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, io.EOF),
		errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.EPIPE),
		errors.Is(err, net.ErrClosed):
		return metrics.DBResultStaleConnection

	default:
		return metrics.DBResultOther
	}
}

// isPostgresError reports whether Postgres itself rejected the statement, which
// says nothing about the health of the connection.
func isPostgresError(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr)
}
