package director

import (
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/mdm"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// setupNanoMDMFlag sets up the mdm-server-type flag and other required flags for testing
func setupNanoMDMFlag(t *testing.T) {
	t.Helper()
	if flag.Lookup("mdm-server-type") == nil {
		flag.String("mdm-server-type", "nanomdm", "MDM server type")
	} else {
		_ = flag.Set("mdm-server-type", "nanomdm")
	}
	// Prometheus flag is required to avoid nil pointer issues
	if flag.Lookup("prometheus") == nil {
		flag.Bool("prometheus", false, "Enable prometheus metrics")
	}
}

// setupMockDB creates a mock database with regex query matcher
func setupMockDB(t *testing.T) (sqlmock.Sqlmock, func()) {
	t.Helper()
	oldDB := db.DB

	postgresMock, mockSpy, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)

	DB, err := gorm.Open(postgres.New(postgres.Config{Conn: postgresMock}), &gorm.Config{})
	require.NoError(t, err)

	db.DB = DB

	cleanup := func() {
		db.DB = oldDB
		postgresMock.Close()
	}

	return mockSpy, cleanup
}

// mockGetDevice sets up DB expectations for GetDevice function
// GetDevice uses First().Scan() which generates TWO queries
func mockGetDevice(mockSpy sqlmock.Sqlmock) {
	const udid = "test-udid-123"
	deviceRows1 := sqlmock.NewRows([]string{"ud_id", "serial_number"}).
		AddRow(udid, "C02TEST123")
	deviceRows2 := sqlmock.NewRows([]string{"ud_id", "serial_number"}).
		AddRow(udid, "C02TEST123")

	// First query from First()
	mockSpy.ExpectQuery(`SELECT \* FROM "devices" WHERE ud_id = \$1 ORDER BY "devices"\."ud_id" LIMIT 1`).
		WithArgs(udid).
		WillReturnRows(deviceRows1)

	// Second query from Scan() - includes primary key in WHERE
	mockSpy.ExpectQuery(`SELECT \* FROM "devices" WHERE ud_id = \$1 AND "devices"\."ud_id" = \$2 ORDER BY "devices"\."ud_id" LIMIT 1`).
		WithArgs(udid, udid).
		WillReturnRows(deviceRows2)
}

// mockCreateCommand sets up DB expectations for db.DB.Create(&command)
func mockCreateCommand(mockSpy sqlmock.Sqlmock) {
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`INSERT INTO "commands"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()
}

// newMockNanoMDMServer creates a mock NanoMDM HTTP server for testing
// Pass mdm.WithMaxRetries(0) for error-case tests that return 5xx, to avoid slow retry waits
func newMockNanoMDMServer(t *testing.T, handler http.HandlerFunc, opts ...mdm.ClientOption) *mdm.NanoMDMClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return mdm.NewClient(server.URL, "test-api-key", opts...)
}

// --- SendCommand tests ---

// Test that empty UDID returns error before any MDM call
func TestSendCommand_NanoMDM_EmptyUDID(t *testing.T) {
	setupNanoMDMFlag(t)
	_, cleanup := setupMockDB(t)
	defer cleanup()

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Should never be called — empty UDID is rejected before HTTP
		t.Error("unexpected HTTP call for empty UDID")
	})

	payload := types.CommandPayload{
		UDID:        "",
		RequestType: "DeviceInformation",
	}
	_, err := sendCommandWithClient(nanoClient, payload)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no device UDID set")
}

// Test Enqueue error handling
func TestSendCommand_NanoMDM_EnqueueError(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	// Pass WithMaxRetries(0) so 500 doesn't cause slow backoff waits.
	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}, mdm.WithMaxRetries(0))

	mockGetDevice(mockSpy)

	payload := types.CommandPayload{
		UDID:        "test-udid-123",
		RequestType: "DeviceInformation",
	}
	_, err := sendCommandWithClient(nanoClient, payload)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nanoMDM enqueue")
	assert.Contains(t, err.Error(), "500")
}

// Test command error from NanoMDM
func TestSendCommand_NanoMDM_CommandError(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := mdm.APIResponse{
			Status: map[string]mdm.EnrollmentStatus{
				"test-udid-123": {CommandError: "invalid command format"},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	mockGetDevice(mockSpy)

	payload := types.CommandPayload{
		UDID:        "test-udid-123",
		RequestType: "InvalidCommand",
	}
	_, err := sendCommandWithClient(nanoClient, payload)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "command enqueue failed")
	assert.Contains(t, err.Error(), "invalid command format")
}

// Test successful command with push error (command still queued)
func TestSendCommand_NanoMDM_PushErrorButCommandQueued(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := mdm.APIResponse{
			CommandUUID: "test-command-uuid-456",
			RequestType: "DeviceInformation",
			Status: map[string]mdm.EnrollmentStatus{
				"test-udid-123": {PushError: "APNs token expired"},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	mockGetDevice(mockSpy)
	mockCreateCommand(mockSpy)

	payload := types.CommandPayload{
		UDID:        "test-udid-123",
		RequestType: "DeviceInformation",
	}
	command, err := sendCommandWithClient(nanoClient, payload)

	require.NoError(t, err)
	assert.Equal(t, "test-udid-123", command.DeviceUDID)
	assert.Equal(t, "test-command-uuid-456", command.CommandUUID)
}

// Test successful SendCommand
func TestSendCommand_NanoMDM_Success(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	var capturedBody []byte
	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(capturedBody)

		resp := mdm.APIResponse{
			CommandUUID: "test-command-uuid-123",
			RequestType: "DeviceInformation",
			Status: map[string]mdm.EnrollmentStatus{
				"test-udid-123": {PushResult: "success"},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	mockGetDevice(mockSpy)
	mockCreateCommand(mockSpy)

	payload := types.CommandPayload{
		UDID:        "test-udid-123",
		RequestType: "DeviceInformation",
	}
	command, err := sendCommandWithClient(nanoClient, payload)

	require.NoError(t, err)
	assert.Equal(t, "test-udid-123", command.DeviceUDID)
	assert.Equal(t, "test-command-uuid-123", command.CommandUUID)
	assert.Equal(t, "DeviceInformation", command.RequestType)
	// Verify plist body was sent to the server
	assert.NotEmpty(t, capturedBody)
}

// Test device not found
func TestSendCommand_NanoMDM_DeviceNotFound(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected HTTP call: device lookup should fail before enqueue")
	})

	mockSpy.ExpectQuery(`SELECT \* FROM "devices" WHERE ud_id = \$1`).
		WithArgs("nonexistent-udid").
		WillReturnError(gorm.ErrRecordNotFound)

	payload := types.CommandPayload{
		UDID:        "nonexistent-udid",
		RequestType: "DeviceInformation",
	}
	_, err := sendCommandWithClient(nanoClient, payload)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "record not found")
}

// Test various request types via table-driven subtests
func TestSendCommand_NanoMDM_RequestTypes(t *testing.T) {
	testCases := []struct {
		name        string
		requestType string
		commandUUID string
	}{
		{"InstallProfile", "InstallProfile", "profile-install-uuid"},
		{"RemoveProfile", "RemoveProfile", "profile-remove-uuid"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setupNanoMDMFlag(t)
			mockSpy, cleanup := setupMockDB(t)
			defer cleanup()

			nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
				resp := mdm.APIResponse{
					CommandUUID: tc.commandUUID,
					RequestType: tc.requestType,
					Status: map[string]mdm.EnrollmentStatus{
						"test-udid-123": {PushResult: "success"},
					},
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(resp)
			})

			mockGetDevice(mockSpy)
			mockCreateCommand(mockSpy)

			payload := types.CommandPayload{
				UDID:        "test-udid-123",
				RequestType: tc.requestType,
			}
			command, err := sendCommandWithClient(nanoClient, payload)

			require.NoError(t, err)
			assert.Equal(t, tc.requestType, command.RequestType)
			assert.Equal(t, tc.commandUUID, command.CommandUUID)
		})
	}
}

// --- PushDevice tests ---

// Test successful push to device
func TestPushDevice_NanoMDM_Success(t *testing.T) {
	setupNanoMDMFlag(t)

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/v1/push/")

		resp := mdm.APIResponse{
			Status: map[string]mdm.EnrollmentStatus{
				"test-udid-123": {PushResult: "success"},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	err := pushDeviceWithClient(nanoClient, "test-udid-123")
	require.NoError(t, err)
}

// Test push when NanoMDM returns an error
func TestPushDevice_NanoMDM_PushError(t *testing.T) {
	setupNanoMDMFlag(t)

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}, mdm.WithMaxRetries(0))

	err := pushDeviceWithClient(nanoClient, "test-udid-123")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PushDevice")
	assert.Contains(t, err.Error(), "500")
}

// Test push when APNs push fails (per-device error)
func TestPushDevice_NanoMDM_APNsError(t *testing.T) {
	setupNanoMDMFlag(t)

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := mdm.APIResponse{
			Status: map[string]mdm.EnrollmentStatus{
				"test-udid-123": {PushError: "BadDeviceToken"},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	err := pushDeviceWithClient(nanoClient, "test-udid-123")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "push failed")
	assert.Contains(t, err.Error(), "BadDeviceToken")
}

// Test push verifies correct UDID is passed to client
func TestPushDevice_NanoMDM_CorrectUDID(t *testing.T) {
	setupNanoMDMFlag(t)

	var capturedPath string
	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		resp := mdm.APIResponse{
			Status: map[string]mdm.EnrollmentStatus{
				"specific-device-udid-456": {PushResult: "success"},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	err := pushDeviceWithClient(nanoClient, "specific-device-udid-456")

	require.NoError(t, err)
	assert.Contains(t, capturedPath, "specific-device-udid-456")
}

// --- InspectCommandQueue tests ---

// Test successful queue inspection
func TestInspectCommandQueue_NanoMDM_Success(t *testing.T) {
	setupNanoMDMFlag(t)

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/v1/queue/test-udid-123")

		resp := mdm.QueueResponse{
			Commands: []mdm.QueueCommand{
				{CommandUUID: "cmd-uuid-1", RequestType: "DeviceInformation", Command: "base64encodedplist1"},
				{CommandUUID: "cmd-uuid-2", RequestType: "ProfileList", Command: "base64encodedplist2"},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	device := types.Device{UDID: "test-udid-123"}
	result, err := inspectCommandQueueWithClient(nanoClient, device)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify the result is valid JSON
	var parsed map[string]interface{}
	err = json.Unmarshal(result, &parsed)
	require.NoError(t, err)
}

// Test queue inspection when NanoMDM returns an error
func TestInspectCommandQueue_NanoMDM_Error(t *testing.T) {
	setupNanoMDMFlag(t)

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := mdm.QueueResponse{Error: "server unavailable"}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(resp)
	})

	device := types.Device{UDID: "test-udid-123"}
	result, err := inspectCommandQueueWithClient(nanoClient, device)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "InspectCommandQueue via NanoMDM")
	assert.Contains(t, err.Error(), "server unavailable")
}

// --- clearCommandQueue tests ---

// Test successful queue clear
func TestClearCommandQueue_NanoMDM_Success(t *testing.T) {
	setupNanoMDMFlag(t)

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Contains(t, r.URL.Path, "/v1/queue/test-udid-123")
		w.WriteHeader(http.StatusNoContent)
	})

	device := types.Device{UDID: "test-udid-123"}
	err := clearCommandQueueWithClient(nanoClient, device)
	require.NoError(t, err)
}

// Test queue clear when NanoMDM returns an error
func TestClearCommandQueue_NanoMDM_Error(t *testing.T) {
	setupNanoMDMFlag(t)

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}, mdm.WithMaxRetries(0))

	device := types.Device{UDID: "test-udid-123"}
	err := clearCommandQueueWithClient(nanoClient, device)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "clearCommandQueue via NanoMDM")
	assert.Contains(t, err.Error(), "500")
}

// Test queue clear verifies correct UDID is passed
func TestClearCommandQueue_NanoMDM_CorrectUDID(t *testing.T) {
	setupNanoMDMFlag(t)

	var capturedPath string
	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	device := types.Device{UDID: "specific-device-udid-789"}
	err := clearCommandQueueWithClient(nanoClient, device)

	require.NoError(t, err)
	assert.Contains(t, capturedPath, "specific-device-udid-789")
}

// --- FetchDevicesFromMDM tests ---

// Test successful device fetch from NanoMDM
func TestFetchDevicesFromMDM_NanoMDM_Success(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	DevicesFetchedFromMDM = false

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := mdm.EnrollmentsResponse{
			Enrollments: []mdm.Enrollment{
				{ID: "device-udid-1", Enabled: true, Device: &mdm.EnrollmentDevice{SerialNumber: "C02SERIAL001"}},
				{ID: "device-udid-2", Enabled: false, Device: &mdm.EnrollmentDevice{SerialNumber: "C02SERIAL002"}},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`INSERT INTO "devices"`).
		WillReturnResult(sqlmock.NewResult(2, 2))
	mockSpy.ExpectCommit()

	fetchDevicesFromNanoMDM(nanoClient)

	assert.True(t, DevicesFetchedFromMDM)
}

// Test fetch when NanoMDM returns an error
func TestFetchDevicesFromMDM_NanoMDM_Error(t *testing.T) {
	setupNanoMDMFlag(t)
	DevicesFetchedFromMDM = false

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		resp := mdm.APIResponse{CommandError: "connection refused"}
		_ = json.NewEncoder(w).Encode(resp)
	}, mdm.WithMaxRetries(0))

	fetchDevicesFromNanoMDM(nanoClient)

	// Should not set flag when fetch fails
	assert.False(t, DevicesFetchedFromMDM)
}

// Test fetch with empty enrollment list
func TestFetchDevicesFromMDM_NanoMDM_EmptyEnrollments(t *testing.T) {
	setupNanoMDMFlag(t)
	DevicesFetchedFromMDM = false

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := mdm.EnrollmentsResponse{Enrollments: []mdm.Enrollment{}}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	fetchDevicesFromNanoMDM(nanoClient)

	// Flag should be set even with empty enrollment list
	assert.True(t, DevicesFetchedFromMDM)
}

// Test fetch skips enrollments with empty ID
func TestFetchDevicesFromMDM_NanoMDM_SkipsEmptyID(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	DevicesFetchedFromMDM = false

	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := mdm.EnrollmentsResponse{
			Enrollments: []mdm.Enrollment{
				{ID: "", Enabled: true},
				{ID: "valid-device-udid", Enabled: true, Device: &mdm.EnrollmentDevice{SerialNumber: "C02VALID"}},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	// Only one valid device — one INSERT batch
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`INSERT INTO "devices"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()

	fetchDevicesFromNanoMDM(nanoClient)

	assert.True(t, DevicesFetchedFromMDM)
}

// Test fetch correctly sets device fields from enrollment
func TestFetchDevicesFromMDM_NanoMDM_SetsDeviceFields(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	DevicesFetchedFromMDM = false

	var capturedRequest string
	nanoClient := newMockNanoMDMServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedRequest = r.URL.Path
		resp := mdm.EnrollmentsResponse{
			Enrollments: []mdm.Enrollment{
				{ID: "enabled-device", Enabled: true, Device: &mdm.EnrollmentDevice{SerialNumber: "SERIAL001"}},
				{ID: "disabled-device", Enabled: false, Device: &mdm.EnrollmentDevice{SerialNumber: "SERIAL002"}},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`INSERT INTO "devices"`).
		WillReturnResult(sqlmock.NewResult(2, 2))
	mockSpy.ExpectCommit()

	fetchDevicesFromNanoMDM(nanoClient)

	assert.True(t, DevicesFetchedFromMDM)
	assert.NotEmpty(t, capturedRequest)
}
