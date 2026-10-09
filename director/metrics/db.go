package metrics

import (
	"database/sql"
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Result labels for dbOperationsTotal. A database call either succeeded or
// failed for a reason worth telling apart, because the response to a dead
// connection is not the response to a constraint violation.
const (
	// DBResultSuccess - the operation completed
	DBResultSuccess = "success"
	// DBResultNotFound - no row matched. A normal outcome, not a fault
	DBResultNotFound = "not_found"
	// DBResultStaleConnection - the connection was dead when it was used.
	// This is the signature of middleware dropping idle connections
	DBResultStaleConnection = "stale_connection"
	// DBResultTimeout - the operation ran out of time
	DBResultTimeout = "timeout"
	// DBResultCanceled - the caller gave up before the operation finished
	DBResultCanceled = "canceled"
	// DBResultPostgresError - Postgres rejected the statement (constraint
	// violation, bad SQL). The connection itself was fine
	DBResultPostgresError = "postgres_error"
	// DBResultOther - anything not classified above
	DBResultOther = "other"
)

// dbOperationsTotal counts gorm operations by kind and outcome
// operation: create, query, update, delete, row, raw
// result: see the DBResult constants
//
//nolint:gochecknoglobals
var dbOperationsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: subsystem,
		Name:      "db_operations_total",
		Help:      "Total database operations by kind and outcome.",
	},
	[]string{"operation", "result"},
)

// DBOperations - accessor for dbOperationsTotal
func DBOperations(operation, result string) prometheus.Counter {
	return dbOperationsTotal.WithLabelValues(operation, result)
}

// RegisterDBStats exposes the connection pool's own counters as go_sql_*
// metrics. These come from database/sql itself and are read at scrape time, so
// they cost nothing until Prometheus asks. They are what shows whether the pool
// is recycling connections, and whether callers are queueing for one.
func RegisterDBStats(pool *sql.DB, dbName string) error {
	err := prometheus.Register(collectors.NewDBStatsCollector(pool, dbName))

	var alreadyRegistered prometheus.AlreadyRegisteredError
	if errors.As(err, &alreadyRegistered) {
		return nil
	}

	return err
}
