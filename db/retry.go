package db

import "github.com/mdmdirector/mdmdirector/director/metrics"

// IsStaleConnection reports whether err means the pooled connection itself died
// (reset by the peer, unexpected EOF, broken pipe) rather than the statement
// being rejected. An in-mesh proxy such as an Istio sidecar can reset a pooled
// connection at any time, and the next statement on it fails this way.
func IsStaleConnection(err error) bool {
	return err != nil && dbResult(err) == metrics.DBResultStaleConnection
}

// RetryRead runs fn and, if it fails on a stale connection, runs it once more.
// database/sql discards the dead connection after the first failure, so the
// second attempt gets a fresh one.
//
// Use it only for read-only statements. A write that fails mid-flight may
// already have been applied by Postgres, so retrying it could apply it twice.
func RetryRead(fn func() error) error {
	err := fn()
	if IsStaleConnection(err) {
		err = fn()
	}
	return err
}
