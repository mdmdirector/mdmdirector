package utils

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPClient(t *testing.T) {
	t.Run("with default config", func(t *testing.T) {
		client := NewHTTPClient(10*time.Second, nil)

		assert.NotNil(t, client)
		assert.NotNil(t, client.client)
		assert.Equal(t, 3, client.retryConfig.MaxRetries)
	})

	t.Run("with custom config", func(t *testing.T) {
		customConfig := &RetryConfig{
			MaxRetries:    5,
			MaxBackoff:    60 * time.Second,
			BackoffFactor: 3.0,
		}
		client := NewHTTPClient(10*time.Second, customConfig)

		assert.Equal(t, 5, client.retryConfig.MaxRetries)
		assert.Equal(t, 60*time.Second, client.retryConfig.MaxBackoff)
		assert.Equal(t, 3.0, client.retryConfig.BackoffFactor)
	})
}

func TestHTTPClient_Do_SuccessfulRequest(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	client := NewHTTPClient(10*time.Second, nil)
	req, err := http.NewRequest("GET", server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(1), atomic.LoadInt32(&requestCount), "Should only make one request on success")

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, `{"status": "ok"}`, string(body))
}

func TestHTTPClient_Do_RetryOn5xxStatusCodes(t *testing.T) {
	testCases := []struct {
		name        string
		statusCode  int
		shouldRetry bool
	}{
		{"500 Internal Server Error", http.StatusInternalServerError, true},
		{"502 Bad Gateway", http.StatusBadGateway, true},
		{"503 Service Unavailable", http.StatusServiceUnavailable, true},
		{"504 Gateway Timeout", http.StatusGatewayTimeout, true},
		{"429 Too Many Requests", http.StatusTooManyRequests, true},
		{"501 Not Implemented", http.StatusNotImplemented, false},
		{"400 Bad Request", http.StatusBadRequest, false},
		{"404 Not Found", http.StatusNotFound, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var requestCount int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requestCount, 1)
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()

			config := &RetryConfig{
				MaxRetries:    2,
				MaxBackoff:    100 * time.Millisecond,
				BackoffFactor: 1.1,
			}
			client := NewHTTPClient(10*time.Second, config)
			req, _ := http.NewRequest("GET", server.URL, nil)

			resp, _ := client.Do(req)
			if resp != nil {
				resp.Body.Close()
			}

			if tc.shouldRetry {
				// Should retry MaxRetries times + initial attempt
				assert.Equal(t, int32(3), atomic.LoadInt32(&requestCount),
					"Should retry on %d status code", tc.statusCode)
			} else {
				assert.Equal(t, int32(1), atomic.LoadInt32(&requestCount),
					"Should NOT retry on %d status code", tc.statusCode)
			}
		})
	}
}

func TestHTTPClient_Do_RetryThenSuccess(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	config := &RetryConfig{
		MaxRetries:    3,
		MaxBackoff:    100 * time.Millisecond,
		BackoffFactor: 1.1,
	}
	client := NewHTTPClient(10*time.Second, config)
	req, _ := http.NewRequest("GET", server.URL, nil)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(3), atomic.LoadInt32(&requestCount), "Should succeed on third attempt")

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "success", string(body))
}

func TestHTTPClient_Do_BodyPreservationAcrossRetries(t *testing.T) {
	var requestBodies []string
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)

		// Read the request body
		body, _ := io.ReadAll(r.Body)
		requestBodies = append(requestBodies, string(body))

		if count < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &RetryConfig{
		MaxRetries:    3,
		MaxBackoff:    100 * time.Millisecond,
		BackoffFactor: 1.1,
	}
	client := NewHTTPClient(10*time.Second, config)

	requestBody := `{"key": "value", "important": "data"}`
	req, _ := http.NewRequest("POST", server.URL, bytes.NewBufferString(requestBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, int32(3), atomic.LoadInt32(&requestCount))

	// Verify body was preserved across all retries
	for i, body := range requestBodies {
		assert.Equal(t, requestBody, body, "Body should be preserved on attempt %d", i+1)
	}
}

func TestHTTPClient_Do_ContextCancellation(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	config := &RetryConfig{
		MaxRetries:    10,
		MaxBackoff:    5 * time.Second,
		BackoffFactor: 2.0,
	}
	client := NewHTTPClient(10*time.Second, config)

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)

	// Cancel context after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := client.Do(req)
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)

	// Should have stopped retrying after context was canceled
	assert.Less(t, atomic.LoadInt32(&requestCount), int32(10),
		"Should stop retrying when context is canceled")
}

func TestHTTPClient_Do_MaxRetriesExhausted(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	config := &RetryConfig{
		MaxRetries:    2,
		MaxBackoff:    100 * time.Millisecond,
		BackoffFactor: 1.1,
	}
	client := NewHTTPClient(10*time.Second, config)
	req, _ := http.NewRequest("GET", server.URL, nil)

	resp, err := client.Do(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after 2 retries")

	// Response should still be returned for inspection
	if resp != nil {
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		resp.Body.Close()
	}

	// Initial attempt + MaxRetries
	assert.Equal(t, int32(3), atomic.LoadInt32(&requestCount))
}

func TestIsRetryableStatusCode(t *testing.T) {
	retryable := []int{
		http.StatusTooManyRequests,     // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout,      // 504
	}

	nonRetryable := []int{
		http.StatusOK,             // 200
		http.StatusCreated,        // 201
		http.StatusBadRequest,     // 400
		http.StatusUnauthorized,   // 401
		http.StatusForbidden,      // 403
		http.StatusNotFound,       // 404
		http.StatusNotImplemented, // 501
	}

	for _, code := range retryable {
		assert.True(t, isRetryableStatusCode(code), "Status %d should be retryable", code)
	}

	for _, code := range nonRetryable {
		assert.False(t, isRetryableStatusCode(code), "Status %d should NOT be retryable", code)
	}
}
