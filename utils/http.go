package utils

import (
	"bytes"
	"io"
	"math"
	"net"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

// RetryConfig - configuration for HTTP retry behavior
type RetryConfig struct {
	MaxRetries    int
	MaxBackoff    time.Duration
	BackoffFactor float64
}

// DefaultRetryConfig - defaults for retry behavior
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:    3,
		MaxBackoff:    30 * time.Second,
		BackoffFactor: 2.0,
	}
}

type HTTPClient struct {
	client      *http.Client
	retryConfig RetryConfig
}

// NewHTTPClient - new HTTP client with retry support
func NewHTTPClient(timeout time.Duration, retryConfig *RetryConfig) *HTTPClient {
	cfg := DefaultRetryConfig()
	if retryConfig != nil {
		cfg = *retryConfig
	}

	return &HTTPClient{
		client: &http.Client{
			Timeout: timeout,
		},
		retryConfig: cfg,
	}
}

// Do - executes the HTTP request with automatic retry for transient failures
// Retries on: network errors, timeouts, and 5xx status codes (except 501)
func (c *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	var lastErr error
	var resp *http.Response

	// Store the original body for potential retries
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read request body")
		}
		req.Body.Close()
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
		req.ContentLength = int64(len(bodyBytes))
	}

	for attempt := 0; attempt <= c.retryConfig.MaxRetries; attempt++ {
		if attempt > 0 {
			waitDuration := c.calculateBackoff(attempt)
			select {
			case <-req.Context().Done():
				// if context is canceled, stop
				return nil, req.Context().Err()
			case <-time.After(waitDuration):
				// wait finished, proceed to retry
			}
		}

		// Recreate the body for each attempt
		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, errors.Wrap(err, "failed to recreate request body")
			}
			req.Body = body
		}

		resp, lastErr = c.client.Do(req)

		// Success - no error and not a retryable status code
		if lastErr == nil && !isRetryableStatusCode(resp.StatusCode) {
			return resp, nil
		}

		// Non-retryable error
		if lastErr != nil && !isRetryableError(lastErr) {
			return nil, errors.Wrap(lastErr, "non-retryable error")
		}

		// Close response body before retry to prevent resource leak
		if resp != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}

	if lastErr != nil {
		return nil, errors.Wrapf(lastErr, "request failed after %d retries", c.retryConfig.MaxRetries)
	}

	// Last attempt returned a retryable status code - return the response
	return resp, errors.Errorf("request failed with status %d after %d retries",
		resp.StatusCode, c.retryConfig.MaxRetries)
}

// calculateBackoff returns backoff duration for the given attempt
func (c *HTTPClient) calculateBackoff(attempt int) time.Duration {
	backoff := float64(time.Second) * math.Pow(c.retryConfig.BackoffFactor, float64(attempt-1))
	if backoff > float64(c.retryConfig.MaxBackoff) {
		backoff = float64(c.retryConfig.MaxBackoff)
	}
	return time.Duration(backoff)
}

// isRetryableStatusCode returns true if the HTTP status code indicates a transient server-side failure
func isRetryableStatusCode(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests, // 429 - Rate limited
		http.StatusInternalServerError, // 500 - Server error
		http.StatusBadGateway,          // 502 - Bad gateway
		http.StatusServiceUnavailable,  // 503 - Service unavailable
		http.StatusGatewayTimeout:      // 504 - Gateway timeout
		return true
	default:
		return false
	}
}

// isRetryableError returns true if the error is a transient network error
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Timeout errors
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// Connection errors (refused, reset, etc.)
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	// DNS errors (temporary)
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.Temporary()
	}

	return false
}
