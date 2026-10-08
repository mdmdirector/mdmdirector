package db

import "github.com/mdmdirector/mdmdirector/director/metrics"

// IsStaleConnection reports whether err means the pooled connection itself died
// (reset by the peer, unexpected EOF, broken pipe) rather than the statement
// being rejected. An in-mesh proxy such as an Istio sidecar can reset a pooled
// connection at any time, and the next statement on it fails this way.
func IsStaleConnection(err error) bool {
	return err != nil && dbResult(err) == metrics.DBResultStaleConnection
}
