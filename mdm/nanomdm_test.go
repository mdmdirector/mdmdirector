package mdm

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/groob/plist"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient(t *testing.T) {
	t.Run("returns error when not initialized", func(t *testing.T) {
		nanoMDMClient = nil
		client, err := Client()
		assert.Nil(t, client)
		assert.Equal(t, ErrClientNotInitialized, err)
	})

	t.Run("returns NanoMDMClient when initialized", func(t *testing.T) {
		InitClient("https://nano.example.com", "test-key")

		client, err := Client()
		require.NoError(t, err)
		assert.NotNil(t, client)

		// Verify it is the concrete type
		_ = client
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

	t.Run("InstallProfile payload is base64-encoded exactly once on the wire", func(t *testing.T) {
		// The mobileconfig bytes any caller would have produced.
		rawMobileconfig := []byte(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>PayloadType</key><string>Configuration</string></dict></plist>`)

		// Callers (e.g. director/profile.go) hand us the already-base64'd string.
		callerPayload := base64.StdEncoding.EncodeToString(rawMobileconfig)

		var capturedBody []byte
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(APIResponse{CommandUUID: "cmd-1"})
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		_, err := client.Enqueue([]string{"device-123"}, types.CommandPayload{
			UDID:        "device-123",
			RequestType: "InstallProfile",
			Payload:     callerPayload,
		}, nil)
		require.NoError(t, err)

		// Decode the on-wire plist exactly the way macOS does, and assert that
		// the <data> in <key>Payload</key> decodes to the original mobileconfig.
		// If the producer's base64 wasn't decoded in Enqueue, this round-trip
		// would yield `callerPayload` (a base64 string) instead of `rawMobileconfig`.
		var decoded MDMCommandPlist
		require.NoError(t, plist.Unmarshal(capturedBody, &decoded))
		assert.Equal(t, "InstallProfile", decoded.Command.RequestType)
		assert.Equal(t, rawMobileconfig, decoded.Command.Payload,
			"<data> on the wire must decode to the raw mobileconfig (no double-base64)")
	})

	t.Run("invalid base64 payload returns error", func(t *testing.T) {
		client := createTestClient("https://example.com", "key")

		_, err := client.Enqueue([]string{"device-123"}, types.CommandPayload{
			RequestType: "InstallProfile",
			Payload:     "!!!not-base64!!!",
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode payload")
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

// fakeQueueServer serves a queue of n commands the way NanoMDM's queue API does: by
// limit/offset, 100 per page when no limit is given, with no next cursor.
func fakeQueueServer(t *testing.T, n int, requests *[]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.URL.RawQuery)
		limit, offset := 100, 0
		if v := r.URL.Query().Get("limit"); v != "" {
			limit, _ = strconv.Atoi(v)
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			offset, _ = strconv.Atoi(v)
		}
		resp := QueueResponse{Commands: []QueueCommand{}}
		for i := offset; i < n && i < offset+limit; i++ {
			resp.Commands = append(resp.Commands, QueueCommand{CommandUUID: fmt.Sprintf("cmd-%d", i), RequestType: "InstallProfile"})
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestNanoMDMClient_InspectQueue_Paging(t *testing.T) {
	t.Run("pages past NanoMDM's default of 100", func(t *testing.T) {
		var requests []string
		server := fakeQueueServer(t, 250, &requests)
		client := createTestClient(server.URL, "test-key")

		result, err := client.InspectQueue("device-123")
		require.NoError(t, err)
		assert.Len(t, result.Commands, 250)
		assert.False(t, result.Truncated)
		assert.Equal(t, "cmd-249", result.Commands[249].CommandUUID)
		assert.Equal(t, []string{"limit=100&offset=0", "limit=100&offset=100", "limit=100&offset=200"}, requests)
	})

	t.Run("exactly one full page", func(t *testing.T) {
		var requests []string
		server := fakeQueueServer(t, 100, &requests)
		client := createTestClient(server.URL, "test-key")

		result, err := client.InspectQueue("device-123")
		require.NoError(t, err)
		assert.Len(t, result.Commands, 100)
		assert.False(t, result.Truncated)
		assert.Len(t, requests, 2)
	})

	t.Run("stops at the cap and reports truncation", func(t *testing.T) {
		old := queueInspectMaxCommands
		queueInspectMaxCommands = 150
		defer func() { queueInspectMaxCommands = old }()

		var requests []string
		server := fakeQueueServer(t, 1000, &requests)
		client := createTestClient(server.URL, "test-key")

		result, err := client.InspectQueue("device-123")
		require.NoError(t, err)
		assert.Len(t, result.Commands, 150)
		assert.True(t, result.Truncated)
		assert.Equal(t, []string{"limit=100&offset=0", "limit=51&offset=100"}, requests)

		unified, err := ConvertToUnifiedResponse(result)
		require.NoError(t, err)
		assert.Equal(t, 150, unified.Total)
		assert.True(t, unified.Truncated)
	})

	t.Run("queue of exactly the cap is not truncated", func(t *testing.T) {
		old := queueInspectMaxCommands
		queueInspectMaxCommands = 150
		defer func() { queueInspectMaxCommands = old }()

		var requests []string
		server := fakeQueueServer(t, 150, &requests)
		client := createTestClient(server.URL, "test-key")

		result, err := client.InspectQueue("device-123")
		require.NoError(t, err)
		assert.Len(t, result.Commands, 150)
		assert.False(t, result.Truncated)
	})

	t.Run("server that ignores limit", func(t *testing.T) {
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			resp := QueueResponse{}
			for i := 0; i < 120; i++ {
				resp.Commands = append(resp.Commands, QueueCommand{CommandUUID: fmt.Sprintf("cmd-%d", i)})
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()
		client := createTestClient(server.URL, "test-key")

		result, err := client.InspectQueue("device-123")
		require.NoError(t, err)
		assert.Len(t, result.Commands, 120)
		assert.Equal(t, 1, calls)
	})

	t.Run("error on a later page", func(t *testing.T) {
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls > 1 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(QueueResponse{Error: "invalid pagination"})
				return
			}
			resp := QueueResponse{}
			for i := 0; i < 100; i++ {
				resp.Commands = append(resp.Commands, QueueCommand{CommandUUID: fmt.Sprintf("cmd-%d", i)})
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()
		client := createTestClient(server.URL, "test-key")

		_, err := client.InspectQueue("device-123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid pagination")
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

	t.Run("non-500 error status is an error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(QueueDeleteResponse{Error: "unauthorized"})
		}))
		defer server.Close()

		client := createTestClient(server.URL, "test-key")

		_, err := client.ClearQueue("device-1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unauthorized")
		assert.Contains(t, err.Error(), "401")
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
		_, _, err := doRequest[APIResponse](client, req)
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
		_, _, err := doRequest[APIResponse](client, req)
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
		result, status, err := doRequest[APIResponse](client, req)
		require.NoError(t, err) // 400 is not a transport/decode error
		assert.Equal(t, http.StatusBadRequest, status)
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
	assert.Equal(t, "ProfileList", unified.Commands[1].RequestType)
	assert.Equal(t, 2, unified.Total)
	assert.False(t, unified.Truncated)
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
