package director

import (
	"encoding/json"
	"flag"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/mdm"
	"github.com/mdmdirector/mdmdirector/mocks"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/pkg/errors"
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
	// Use loose matching - just expect an INSERT into commands and return a row
	mockSpy.ExpectExec(`INSERT INTO "commands"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()
}

// SendCommand Tests

// Test that empty UDID returns error before any MDM call
func TestSendCommand_NanoMDM_EmptyUDID(t *testing.T) {
	setupNanoMDMFlag(t)
	_, cleanup := setupMockDB(t)
	defer cleanup()

	mockClient := &mocks.MockMDMClient{}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	payload := types.CommandPayload{
		UDID:        "",
		RequestType: "DeviceInformation",
	}
	_, err := SendCommand(payload)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no device UDID set")
	assert.Len(t, mockClient.EnqueueCalls, 0)
}

// Test that client not initialized returns proper error
func TestSendCommand_NanoMDM_ClientNotInitialized(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	// Don't set any client - simulate uninitialized state
	mdm.SetClientForTesting(nil)

	// Mock GetDevice returning a device
	mockGetDevice(mockSpy)

	payload := types.CommandPayload{
		UDID:        "test-udid-123",
		RequestType: "DeviceInformation",
	}
	_, err := SendCommand(payload)

	require.Error(t, err)
	assert.Equal(t, mdm.ErrClientNotInitialized, err)
}

// Test Enqueue error handling
func TestSendCommand_NanoMDM_EnqueueError(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockClient := &mocks.MockMDMClient{
		EnqueueFunc: func(enrollmentIDs []string, payload types.CommandPayload, opts *mdm.EnqueueOptions) (*mdm.APIResponse, error) {
			return nil, errors.New("connection refused")
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	mockGetDevice(mockSpy)

	payload := types.CommandPayload{
		UDID:        "test-udid-123",
		RequestType: "DeviceInformation",
	}
	_, err := SendCommand(payload)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nanoMDM enqueue")
	assert.Contains(t, err.Error(), "connection refused")
	require.Len(t, mockClient.EnqueueCalls, 1)
}

// Test command error from NanoMDM
func TestSendCommand_NanoMDM_CommandError(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockClient := &mocks.MockMDMClient{
		EnqueueFunc: func(enrollmentIDs []string, payload types.CommandPayload, opts *mdm.EnqueueOptions) (*mdm.APIResponse, error) {
			return &mdm.APIResponse{
				Status: map[string]mdm.EnrollmentStatus{
					enrollmentIDs[0]: {CommandError: "invalid command format"},
				},
			}, nil
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	mockGetDevice(mockSpy)

	payload := types.CommandPayload{
		UDID:        "test-udid-123",
		RequestType: "InvalidCommand",
	}
	_, err := SendCommand(payload)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "command enqueue failed")
	assert.Contains(t, err.Error(), "invalid command format")
}

// Test successful command with push error (command still queued)
func TestSendCommand_NanoMDM_PushErrorButCommandQueued(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockClient := &mocks.MockMDMClient{
		EnqueueFunc: func(enrollmentIDs []string, payload types.CommandPayload, opts *mdm.EnqueueOptions) (*mdm.APIResponse, error) {
			return &mdm.APIResponse{
				CommandUUID: "test-command-uuid-456",
				RequestType: payload.RequestType,
				Status: map[string]mdm.EnrollmentStatus{
					enrollmentIDs[0]: {PushError: "APNs token expired"},
				},
			}, nil
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	mockGetDevice(mockSpy)
	mockCreateCommand(mockSpy)

	payload := types.CommandPayload{
		UDID:        "test-udid-123",
		RequestType: "DeviceInformation",
	}
	command, err := SendCommand(payload)

	require.NoError(t, err)
	assert.Equal(t, "test-udid-123", command.DeviceUDID)
	assert.Equal(t, "test-command-uuid-456", command.CommandUUID)
}

// Test successful SendCommand
func TestSendCommand_NanoMDM_Success(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockClient := &mocks.MockMDMClient{
		EnqueueFunc: func(enrollmentIDs []string, payload types.CommandPayload, opts *mdm.EnqueueOptions) (*mdm.APIResponse, error) {
			return &mdm.APIResponse{
				CommandUUID: "test-command-uuid-123",
				RequestType: payload.RequestType,
				Status: map[string]mdm.EnrollmentStatus{
					enrollmentIDs[0]: {PushResult: "success"},
				},
			}, nil
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	mockGetDevice(mockSpy)
	mockCreateCommand(mockSpy)

	payload := types.CommandPayload{
		UDID:        "test-udid-123",
		RequestType: "DeviceInformation",
	}
	command, err := SendCommand(payload)

	require.NoError(t, err)
	assert.Equal(t, "test-udid-123", command.DeviceUDID)
	assert.Equal(t, "test-command-uuid-123", command.CommandUUID)
	assert.Equal(t, "DeviceInformation", command.RequestType)

	require.Len(t, mockClient.EnqueueCalls, 1)
	assert.Equal(t, []string{"test-udid-123"}, mockClient.EnqueueCalls[0].EnrollmentIDs)
	assert.Equal(t, "DeviceInformation", mockClient.EnqueueCalls[0].Payload.RequestType)
}

// Test device not found
func TestSendCommand_NanoMDM_DeviceNotFound(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockClient := &mocks.MockMDMClient{}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	// Mock GetDevice - return error on first query
	mockSpy.ExpectQuery(`SELECT \* FROM "devices" WHERE ud_id = \$1`).
		WithArgs("nonexistent-udid").
		WillReturnError(gorm.ErrRecordNotFound)

	payload := types.CommandPayload{
		UDID:        "nonexistent-udid",
		RequestType: "DeviceInformation",
	}
	_, err := SendCommand(payload)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "record not found")
	assert.Len(t, mockClient.EnqueueCalls, 0)
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

			mockClient := &mocks.MockMDMClient{
				EnqueueFunc: func(enrollmentIDs []string, payload types.CommandPayload, opts *mdm.EnqueueOptions) (*mdm.APIResponse, error) {
					return &mdm.APIResponse{
						CommandUUID: tc.commandUUID,
						RequestType: payload.RequestType,
						Status: map[string]mdm.EnrollmentStatus{
							enrollmentIDs[0]: {PushResult: "success"},
						},
					}, nil
				},
			}
			mdm.SetClientForTesting(mockClient)
			defer mdm.SetClientForTesting(nil)

			mockGetDevice(mockSpy)
			mockCreateCommand(mockSpy)

			payload := types.CommandPayload{
				UDID:        "test-udid-123",
				RequestType: tc.requestType,
			}
			command, err := SendCommand(payload)

			require.NoError(t, err)
			assert.Equal(t, tc.requestType, command.RequestType)
			assert.Equal(t, tc.commandUUID, command.CommandUUID)
		})
	}
}

// PushDevice Tests

// Test successful push to device
func TestPushDevice_NanoMDM_Success(t *testing.T) {
	setupNanoMDMFlag(t)

	mockClient := &mocks.MockMDMClient{
		PushFunc: func(enrollmentIDs ...string) (*mdm.APIResponse, error) {
			return &mdm.APIResponse{
				Status: map[string]mdm.EnrollmentStatus{
					enrollmentIDs[0]: {PushResult: "success"},
				},
			}, nil
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	err := PushDevice("test-udid-123")

	require.NoError(t, err)
	require.Len(t, mockClient.PushCalls, 1)
	assert.Equal(t, []string{"test-udid-123"}, mockClient.PushCalls[0])
}

// Test push when client is not initialized
func TestPushDevice_NanoMDM_ClientNotInitialized(t *testing.T) {
	setupNanoMDMFlag(t)

	mdm.SetClientForTesting(nil)

	err := PushDevice("test-udid-123")

	require.Error(t, err)
	assert.Equal(t, mdm.ErrClientNotInitialized, err)
}

// Test push when NanoMDM returns an error
func TestPushDevice_NanoMDM_PushError(t *testing.T) {
	setupNanoMDMFlag(t)

	mockClient := &mocks.MockMDMClient{
		PushFunc: func(enrollmentIDs ...string) (*mdm.APIResponse, error) {
			return nil, errors.New("connection timeout")
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	err := PushDevice("test-udid-123")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PushDevice")
	assert.Contains(t, err.Error(), "connection timeout")
}

// Test push when APNs push fails (per-device error)
func TestPushDevice_NanoMDM_APNsError(t *testing.T) {
	setupNanoMDMFlag(t)

	mockClient := &mocks.MockMDMClient{
		PushFunc: func(enrollmentIDs ...string) (*mdm.APIResponse, error) {
			return &mdm.APIResponse{
				Status: map[string]mdm.EnrollmentStatus{
					enrollmentIDs[0]: {PushError: "BadDeviceToken"},
				},
			}, nil
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	err := PushDevice("test-udid-123")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "push failed")
	assert.Contains(t, err.Error(), "BadDeviceToken")
}

// Test push verifies correct UDID is passed to client
func TestPushDevice_NanoMDM_CorrectUDID(t *testing.T) {
	setupNanoMDMFlag(t)

	var capturedUDID string
	mockClient := &mocks.MockMDMClient{
		PushFunc: func(enrollmentIDs ...string) (*mdm.APIResponse, error) {
			capturedUDID = enrollmentIDs[0]
			return &mdm.APIResponse{
				Status: map[string]mdm.EnrollmentStatus{
					enrollmentIDs[0]: {PushResult: "success"},
				},
			}, nil
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	err := PushDevice("specific-device-udid-456")

	require.NoError(t, err)
	assert.Equal(t, "specific-device-udid-456", capturedUDID)
}

// InspectCommandQueue Tests

// Test successful queue inspection
func TestInspectCommandQueue_NanoMDM_Success(t *testing.T) {
	setupNanoMDMFlag(t)

	mockClient := &mocks.MockMDMClient{
		InspectQueueFunc: func(enrollmentID string) (*mdm.QueueResponse, error) {
			return &mdm.QueueResponse{
				Commands: []mdm.QueueCommand{
					{
						CommandUUID: "cmd-uuid-1",
						RequestType: "DeviceInformation",
						Command:     "base64encodedplist1",
					},
					{
						CommandUUID: "cmd-uuid-2",
						RequestType: "ProfileList",
						Command:     "base64encodedplist2",
					},
				},
			}, nil
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	device := types.Device{UDID: "test-udid-123"}
	result, err := InspectCommandQueue(device)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify the result is valid JSON
	var parsed map[string]interface{}
	err = json.Unmarshal(result, &parsed)
	require.NoError(t, err)

	// Verify mock was called with correct UDID
	require.Len(t, mockClient.InspectQueueCalls, 1)
	assert.Equal(t, "test-udid-123", mockClient.InspectQueueCalls[0])
}

// Test queue inspection when NanoMDM returns an error
func TestInspectCommandQueue_NanoMDM_Error(t *testing.T) {
	setupNanoMDMFlag(t)

	mockClient := &mocks.MockMDMClient{
		InspectQueueFunc: func(enrollmentID string) (*mdm.QueueResponse, error) {
			return nil, errors.New("server unavailable")
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	device := types.Device{UDID: "test-udid-123"}
	result, err := InspectCommandQueue(device)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "InspectCommandQueue via NanoMDM")
	assert.Contains(t, err.Error(), "server unavailable")
}

// clearCommandQueue Tests

// Test successful queue clear
func TestClearCommandQueue_NanoMDM_Success(t *testing.T) {
	setupNanoMDMFlag(t)

	mockClient := &mocks.MockMDMClient{
		ClearQueueFunc: func(enrollmentIDs ...string) (*mdm.QueueDeleteResponse, error) {
			return &mdm.QueueDeleteResponse{}, nil
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	device := types.Device{UDID: "test-udid-123"}
	err := clearCommandQueue(device)

	require.NoError(t, err)

	// Verify mock was called with correct UDID
	require.Len(t, mockClient.ClearQueueCalls, 1)
	assert.Equal(t, []string{"test-udid-123"}, mockClient.ClearQueueCalls[0])
}

// Test queue clear when NanoMDM returns an error
func TestClearCommandQueue_NanoMDM_Error(t *testing.T) {
	setupNanoMDMFlag(t)

	mockClient := &mocks.MockMDMClient{
		ClearQueueFunc: func(enrollmentIDs ...string) (*mdm.QueueDeleteResponse, error) {
			return nil, errors.New("permission denied")
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	device := types.Device{UDID: "test-udid-123"}
	err := clearCommandQueue(device)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "clearCommandQueue via NanoMDM")
	assert.Contains(t, err.Error(), "permission denied")
}

// Test queue clear verifies correct UDID is passed
func TestClearCommandQueue_NanoMDM_CorrectUDID(t *testing.T) {
	setupNanoMDMFlag(t)

	var capturedUDID string
	mockClient := &mocks.MockMDMClient{
		ClearQueueFunc: func(enrollmentIDs ...string) (*mdm.QueueDeleteResponse, error) {
			capturedUDID = enrollmentIDs[0]
			return &mdm.QueueDeleteResponse{}, nil
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	device := types.Device{UDID: "specific-device-udid-789"}
	err := clearCommandQueue(device)

	require.NoError(t, err)
	assert.Equal(t, "specific-device-udid-789", capturedUDID)
}

// FetchDevicesFromMDM Tests

// Test successful device fetch from NanoMDM
func TestFetchDevicesFromMDM_NanoMDM_Success(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	// Reset the global flag
	DevicesFetchedFromMDM = false

	mockClient := &mocks.MockMDMClient{
		GetAllEnrollmentsFunc: func(config *mdm.PaginationConfig) (*mdm.EnrollmentsResponse, error) {
			return &mdm.EnrollmentsResponse{
				Enrollments: []mdm.Enrollment{
					{
						ID:      "device-udid-1",
						Enabled: true,
						Device: &mdm.EnrollmentDevice{
							SerialNumber: "C02SERIAL001",
						},
					},
					{
						ID:      "device-udid-2",
						Enabled: false,
						Device: &mdm.EnrollmentDevice{
							SerialNumber: "C02SERIAL002",
						},
					},
				},
			}, nil
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	// Mock DB expectations for CreateInBatches with ON CONFLICT
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`INSERT INTO "devices"`).
		WillReturnResult(sqlmock.NewResult(2, 2))
	mockSpy.ExpectCommit()

	FetchDevicesFromMDM()

	// Verify mock was called
	require.Len(t, mockClient.GetAllEnrollmentCalls, 1)
	assert.True(t, DevicesFetchedFromMDM)
}

// Test fetch when client is not initialized
func TestFetchDevicesFromMDM_NanoMDM_ClientNotInitialized(t *testing.T) {
	setupNanoMDMFlag(t)

	// Reset the global flag
	DevicesFetchedFromMDM = false

	mdm.SetClientForTesting(nil)

	FetchDevicesFromMDM()

	// Should not set flag when client fails
	assert.False(t, DevicesFetchedFromMDM)
}

// Test fetch when NanoMDM returns an error
func TestFetchDevicesFromMDM_NanoMDM_Error(t *testing.T) {
	setupNanoMDMFlag(t)

	// Reset the global flag
	DevicesFetchedFromMDM = false

	mockClient := &mocks.MockMDMClient{
		GetAllEnrollmentsFunc: func(config *mdm.PaginationConfig) (*mdm.EnrollmentsResponse, error) {
			return nil, errors.New("connection refused")
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	FetchDevicesFromMDM()

	// Should not set flag when fetch fails
	assert.False(t, DevicesFetchedFromMDM)
	require.Len(t, mockClient.GetAllEnrollmentCalls, 1)
}

// Test fetch with empty enrollment list
func TestFetchDevicesFromMDM_NanoMDM_EmptyEnrollments(t *testing.T) {
	setupNanoMDMFlag(t)

	// Reset the global flag
	DevicesFetchedFromMDM = false

	mockClient := &mocks.MockMDMClient{
		GetAllEnrollmentsFunc: func(config *mdm.PaginationConfig) (*mdm.EnrollmentsResponse, error) {
			return &mdm.EnrollmentsResponse{
				Enrollments: []mdm.Enrollment{},
			}, nil
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	FetchDevicesFromMDM()

	// Should still set flag even with no enrollments
	assert.True(t, DevicesFetchedFromMDM)
}

// Test fetch skips enrollments with empty ID
func TestFetchDevicesFromMDM_NanoMDM_SkipsEmptyID(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	// Reset the global flag
	DevicesFetchedFromMDM = false

	mockClient := &mocks.MockMDMClient{
		GetAllEnrollmentsFunc: func(config *mdm.PaginationConfig) (*mdm.EnrollmentsResponse, error) {
			return &mdm.EnrollmentsResponse{
				Enrollments: []mdm.Enrollment{
					{
						ID:      "", // Empty ID - should be skipped
						Enabled: true,
					},
					{
						ID:      "valid-device-udid",
						Enabled: true,
						Device: &mdm.EnrollmentDevice{
							SerialNumber: "C02VALID",
						},
					},
				},
			}, nil
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	// Only expect DB call for the valid device (not the empty ID one)
	// CreateInBatches wraps in a transaction
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`INSERT INTO "devices"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mockSpy.ExpectCommit()

	FetchDevicesFromMDM()

	assert.True(t, DevicesFetchedFromMDM)
}

// Test fetch correctly sets device fields from enrollment
func TestFetchDevicesFromMDM_NanoMDM_SetsDeviceFields(t *testing.T) {
	setupNanoMDMFlag(t)
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	// Reset the global flag
	DevicesFetchedFromMDM = false

	var capturedEnrollments []mdm.Enrollment
	mockClient := &mocks.MockMDMClient{
		GetAllEnrollmentsFunc: func(config *mdm.PaginationConfig) (*mdm.EnrollmentsResponse, error) {
			enrollments := []mdm.Enrollment{
				{
					ID:      "enabled-device",
					Enabled: true,
					Device: &mdm.EnrollmentDevice{
						SerialNumber: "SERIAL001",
					},
				},
				{
					ID:      "disabled-device",
					Enabled: false,
					Device: &mdm.EnrollmentDevice{
						SerialNumber: "SERIAL002",
					},
				},
			}
			capturedEnrollments = enrollments
			return &mdm.EnrollmentsResponse{Enrollments: enrollments}, nil
		},
	}
	mdm.SetClientForTesting(mockClient)
	defer mdm.SetClientForTesting(nil)

	// Mock DB expectations for CreateInBatches with ON CONFLICT (both devices in one batch)
	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`INSERT INTO "devices"`).
		WillReturnResult(sqlmock.NewResult(2, 2))
	mockSpy.ExpectCommit()

	FetchDevicesFromMDM()

	// Verify enrollments were processed
	require.Len(t, capturedEnrollments, 2)
	assert.Equal(t, "enabled-device", capturedEnrollments[0].ID)
	assert.True(t, capturedEnrollments[0].Enabled)
	assert.Equal(t, "disabled-device", capturedEnrollments[1].ID)
	assert.False(t, capturedEnrollments[1].Enabled)
	assert.True(t, DevicesFetchedFromMDM)
}
