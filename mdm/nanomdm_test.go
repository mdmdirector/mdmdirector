package mdm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testMockClient - inline mock for testing SetClientForTesting
type testMockClient struct{}

func (m *testMockClient) Push(enrollmentIDs ...string) (*APIResponse, error) {
	return &APIResponse{}, nil
}
func (m *testMockClient) Enqueue(enrollmentIDs []string, payload types.CommandPayload, opts *EnqueueOptions) (*APIResponse, error) {
	return &APIResponse{}, nil
}
func (m *testMockClient) InspectQueue(enrollmentID string) (*QueueResponse, error) {
	return &QueueResponse{}, nil
}
func (m *testMockClient) ClearQueue(enrollmentIDs ...string) (*QueueDeleteResponse, error) {
	return &QueueDeleteResponse{}, nil
}
func (m *testMockClient) QueryEnrollments(filter *EnrollmentFilter, config *PaginationConfig) (*EnrollmentsResponse, error) {
	return &EnrollmentsResponse{}, nil
}
func (m *testMockClient) GetAllEnrollments(config *PaginationConfig) (*EnrollmentsResponse, error) {
	return &EnrollmentsResponse{}, nil
}

func TestClient(t *testing.T) {
	t.Run("returns error when not initialized", func(t *testing.T) {
		mdmClient = nil
		client, err := Client()
		assert.Nil(t, client)
		assert.Equal(t, ErrClientNotInitialized, err)
	})

	t.Run("returns client when initialized", func(t *testing.T) {
		InitClient("https://nano.example.com", "test-key")

		client, err := Client()
		require.NoError(t, err)
		assert.NotNil(t, client)
	})
}

func TestSetClientForTesting(t *testing.T) {
	t.Run("allows injecting mock client", func(t *testing.T) {
		// Reset first
		mdmClient = nil

		mock := &testMockClient{}
		SetClientForTesting(mock)

		client, err := Client()
		require.NoError(t, err)
		assert.Equal(t, mock, client)
	})

	t.Run("allows resetting client to nil", func(t *testing.T) {
		InitClient("https://nano.example.com", "test-key")

		SetClientForTesting(nil)

		client, err := Client()
		assert.Nil(t, client)
		assert.Equal(t, ErrClientNotInitialized, err)
	})
}

func TestNanoMDMClient_BuildURL(t *testing.T) {
	client := &NanoMDMClient{
		serverURL: "https://nano.example.com",
		apiKey:    "test-key",
	}

	testCases := []struct {
		name        string
		endpoint    string
		ids         []string
		queryParams map[string]string
		expected    string
	}{
		{
			name:     "single ID",
			endpoint: "push",
			ids:      []string{"device-123"},
			expected: "https://nano.example.com/v1/push/device-123",
		},
		{
			name:     "multiple IDs",
			endpoint: "push",
			ids:      []string{"device-1", "device-2", "device-3"},
			expected: "https://nano.example.com/v1/push/device-1,device-2,device-3",
		},
		{
			name:        "with query params",
			endpoint:    "enqueue",
			ids:         []string{"device-123"},
			queryParams: map[string]string{"nopush": "1"},
			expected:    "https://nano.example.com/v1/enqueue/device-123?nopush=1",
		},
		{
			name:     "empty IDs",
			endpoint: "push",
			ids:      []string{},
			expected: "https://nano.example.com/v1/push",
		},
		{
			name:     "queue endpoint",
			endpoint: "queue",
			ids:      []string{"device-456"},
			expected: "https://nano.example.com/v1/queue/device-456",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := client.buildURL(tc.endpoint, tc.ids, tc.queryParams)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestNanoMDMClient_Push(t *testing.T) {
	t.Run("successful push", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Contains(t, r.URL.Path, "/v1/push/")

			// Verify basic auth
			user, pass, ok := r.BasicAuth()
			assert.True(t, ok)
			assert.Equal(t, NanoMDMAuthUsername, user)
			assert.Equal(t, "test-api-key", pass)

			// Return success response
			resp := APIResponse{
				Status: map[string]EnrollmentStatus{
					"device-123": {PushResult: "success"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-api-key")

		result, err := client.Push("device-123")
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "success", result.Status["device-123"].PushResult)
	})

	t.Run("push with multiple IDs", func(t *testing.T) {
		var capturedPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(APIResponse{})
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		_, err := client.Push("device-1", "device-2", "device-3")
		require.NoError(t, err)
		assert.Contains(t, capturedPath, "device-1,device-2,device-3")
	})

	t.Run("push with no IDs returns error", func(t *testing.T) {
		client := createTestClient("https://example.com", "key")

		_, err := client.Push()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no enrollment IDs provided")
	})

	t.Run("push with client error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := APIResponse{
				PushError: "invalid enrollment ID",
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		result, err := client.Push("device-123")
		require.NoError(t, err) // 400 is not an error at HTTP level
		assert.NotNil(t, result)
		assert.Equal(t, "invalid enrollment ID", result.PushError)
	})
}

func TestNanoMDMClient_Enqueue(t *testing.T) {
	t.Run("successful enqueue", func(t *testing.T) {
		var capturedBody []byte
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPut, r.Method)
			assert.Equal(t, "application/xml", r.Header.Get("Content-Type"))

			capturedBody, _ = io.ReadAll(r.Body)

			resp := APIResponse{
				CommandUUID: "cmd-uuid-123",
				RequestType: "DeviceInformation",
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		payload := types.CommandPayload{
			UDID:        "device-123",
			RequestType: "DeviceInformation",
			Queries:     []string{"SerialNumber", "UDID"},
		}

		result, err := client.Enqueue([]string{"device-123"}, payload, nil)
		require.NoError(t, err)
		assert.Equal(t, "cmd-uuid-123", result.CommandUUID)
		assert.Equal(t, "DeviceInformation", result.RequestType)

		// Verify plist was sent
		assert.Contains(t, string(capturedBody), "DeviceInformation")
	})

	t.Run("enqueue with nopush option", func(t *testing.T) {
		var capturedQuery string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.RawQuery
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(APIResponse{})
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		opts := &EnqueueOptions{NoPush: true}
		_, err := client.Enqueue([]string{"device-123"}, types.CommandPayload{RequestType: "Test"}, opts)
		require.NoError(t, err)
		assert.Contains(t, capturedQuery, "nopush=1")
	})

	t.Run("enqueue with no IDs returns error", func(t *testing.T) {
		client := createTestClient("https://example.com", "key")

		_, err := client.Enqueue([]string{}, types.CommandPayload{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no enrollment IDs provided")
	})
}

func TestNanoMDMClient_InspectQueue(t *testing.T) {
	t.Run("successful inspect", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Contains(t, r.URL.Path, "/v1/queue/device-123")

			resp := QueueResponse{
				Commands: []QueueCommand{
					{CommandUUID: "cmd-1", RequestType: "DeviceInformation"},
					{CommandUUID: "cmd-2", RequestType: "ProfileList"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		result, err := client.InspectQueue("device-123")
		require.NoError(t, err)
		assert.Len(t, result.Commands, 2)
		assert.Equal(t, "cmd-1", result.Commands[0].CommandUUID)
	})

	t.Run("inspect with error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := QueueResponse{
				Error: "device not found",
			}
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		_, err := client.InspectQueue("unknown-device")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "device not found")
	})
}

func TestNanoMDMClient_ClearQueue(t *testing.T) {
	t.Run("successful clear with 204", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodDelete, r.Method)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		result, err := client.ClearQueue("device-123")
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("clear multiple devices", func(t *testing.T) {
		var capturedPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		_, err := client.ClearQueue("device-1", "device-2")
		require.NoError(t, err)
		assert.Contains(t, capturedPath, "device-1,device-2")
	})

	t.Run("partial success with 207", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := QueueDeleteResponse{
				Status: map[string]string{
					"device-1": "",
					"device-2": "device not found",
				},
			}
			w.WriteHeader(http.StatusMultiStatus)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		result, err := client.ClearQueue("device-1", "device-2")
		require.NoError(t, err) // 207 is not an error
		assert.Equal(t, "device not found", result.Status["device-2"])
	})

	t.Run("clear with no IDs returns error", func(t *testing.T) {
		client := createTestClient("https://example.com", "key")

		_, err := client.ClearQueue()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no enrollment IDs provided")
	})
}

func TestNanoMDMClient_QueryEnrollments(t *testing.T) {
	t.Run("successful query", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "/v1/enrollments/query")
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			resp := EnrollmentsResponse{
				Enrollments: []Enrollment{
					{ID: "enroll-1", Type: "Device", Enabled: true},
					{ID: "enroll-2", Type: "Device", Enabled: true},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		filter := &EnrollmentFilter{
			Types: []string{"Device"},
		}
		result, err := client.QueryEnrollments(filter, nil)
		require.NoError(t, err)
		assert.Len(t, result.Enrollments, 2)
	})

	t.Run("query with pagination", func(t *testing.T) {
		var capturedQuery string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.RawQuery
			resp := EnrollmentsResponse{
				Enrollments: []Enrollment{}, // Empty to stop pagination
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		config := &PaginationConfig{PageSize: 100}
		_, err := client.QueryEnrollments(nil, config)
		require.NoError(t, err)
		assert.Contains(t, capturedQuery, "limit=100")
	})
}

func TestNanoMDMClient_GetAllEnrollments(t *testing.T) {
	t.Run("successful get all", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := EnrollmentsResponse{
				Enrollments: []Enrollment{
					{ID: "enroll-1", Type: "Device"},
					{ID: "enroll-2", Type: "User"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		result, err := client.GetAllEnrollments(nil)
		require.NoError(t, err)
		assert.Len(t, result.Enrollments, 2)
	})
}

func TestNanoMDMClient_DoRequest(t *testing.T) {
	t.Run("sets basic auth header", func(t *testing.T) {
		var capturedAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(APIResponse{})
		}))
		defer server.Close()

		client := createTestClient(server.URL, "my-secret-key")

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/push/test", nil)
		_, err := client.doRequest(req)
		require.NoError(t, err)
		assert.Contains(t, capturedAuth, "Basic")
	})

	t.Run("handles invalid JSON response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not valid json"))
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/test", nil)
		_, err := client.doRequest(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode response")
	})

	t.Run("returns response with errors on non-retryable status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := APIResponse{
				CommandError: "bad request error",
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/test", nil)
		result, err := client.doRequest(req)
		require.NoError(t, err) // 400 is not a server error
		assert.NotNil(t, result)
		assert.Equal(t, "bad request error", result.CommandError)
	})
}

func TestAPIResponse_HasErrors(t *testing.T) {
	testCases := []struct {
		name     string
		response APIResponse
		expected bool
	}{
		{
			name:     "no errors",
			response: APIResponse{},
			expected: false,
		},
		{
			name:     "top-level push error",
			response: APIResponse{PushError: "connection failed"},
			expected: true,
		},
		{
			name:     "top-level command error",
			response: APIResponse{CommandError: "invalid command"},
			expected: true,
		},
		{
			name: "per-enrollment push error",
			response: APIResponse{
				Status: map[string]EnrollmentStatus{
					"device-1": {PushError: "token expired"},
				},
			},
			expected: true,
		},
		{
			name: "per-enrollment command error",
			response: APIResponse{
				Status: map[string]EnrollmentStatus{
					"device-1": {CommandError: "failed"},
				},
			},
			expected: true,
		},
		{
			name: "mixed success and error",
			response: APIResponse{
				Status: map[string]EnrollmentStatus{
					"device-1": {PushResult: "success"},
					"device-2": {PushError: "failed"},
				},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.response.HasErrors())
		})
	}
}

func TestAPIResponse_ErrorsForID(t *testing.T) {
	response := APIResponse{
		Status: map[string]EnrollmentStatus{
			"device-1": {PushError: "push failed", CommandError: "cmd failed"},
			"device-2": {PushResult: "success"},
		},
	}

	t.Run("returns errors for existing ID", func(t *testing.T) {
		pushErr, cmdErr := response.ErrorsForID("device-1")
		assert.Equal(t, "push failed", pushErr)
		assert.Equal(t, "cmd failed", cmdErr)
	})

	t.Run("returns empty for successful ID", func(t *testing.T) {
		pushErr, cmdErr := response.ErrorsForID("device-2")
		assert.Empty(t, pushErr)
		assert.Empty(t, cmdErr)
	})

	t.Run("returns empty for unknown ID", func(t *testing.T) {
		pushErr, cmdErr := response.ErrorsForID("unknown")
		assert.Empty(t, pushErr)
		assert.Empty(t, cmdErr)
	})
}

func TestConvertToUnifiedResponse(t *testing.T) {
	nanoResp := &QueueResponse{
		Commands: []QueueCommand{
			{CommandUUID: "cmd-1", RequestType: "DeviceInfo", Command: "base64data1"},
			{CommandUUID: "cmd-2", RequestType: "ProfileList", Command: "base64data2"},
		},
	}

	unified, err := ConvertToUnifiedResponse(nanoResp)
	require.NoError(t, err)

	assert.Len(t, unified.Commands, 2)
	assert.Equal(t, "cmd-1", unified.Commands[0].UUID)
	assert.Equal(t, "base64data1", unified.Commands[0].Payload)
	assert.Equal(t, "cmd-2", unified.Commands[1].UUID)
}

// createTestClient creates a NanoMDMClient for testing with a custom server URL
func createTestClient(serverURL, apiKey string) *NanoMDMClient {
	serverURL = strings.TrimRight(serverURL, "/")
	return &NanoMDMClient{
		serverURL: serverURL,
		apiKey:    apiKey,
		client:    createTestHTTPClient(),
	}
}

// createTestHTTPClient creates an HTTP client suitable for testing
func createTestHTTPClient() *utils.HTTPClient {
	return utils.NewHTTPClient(10*time.Second, &utils.RetryConfig{
		MaxRetries:    0, // No retries in tests for faster execution
		MaxBackoff:    time.Millisecond,
		BackoffFactor: 1.0,
	})
}
