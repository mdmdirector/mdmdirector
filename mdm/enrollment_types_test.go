package mdm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsUserChannelEnrollmentID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{
			name: "device UDID is not a user channel",
			id:   "6F1A0F12-A055-5EF0-B534-E0A6D0770BA5",
			want: false,
		},
		{
			name: "device UDID joined to a user ID is a user channel",
			id:   "6F1A0F12-A055-5EF0-B534-E0A6D0770BA5:E21CD48A-064F-434D-BB05-9E195044FB55",
			want: true,
		},
		{
			name: "empty ID is not a user channel",
			id:   "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsUserChannelEnrollmentID(tt.id))
		})
	}
}

// TestDeviceChannelEnrollmentTypesFilter asserts the device-channel type allowlist is
// serialised into the enrollment query body, so the server can exclude user channels
// before they reach us.
func TestDeviceChannelEnrollmentTypesFilter(t *testing.T) {
	assert.NotContains(t, DeviceChannelEnrollmentTypes, EnrollmentTypeUser)
	assert.NotContains(t, DeviceChannelEnrollmentTypes, EnrollmentTypeUserEnrollment)
	assert.NotContains(t, DeviceChannelEnrollmentTypes, EnrollmentTypeSharedIPad)

	var capturedFilter *EnrollmentFilter
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody EnrollmentQueryRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))
		capturedFilter = reqBody.Filter

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(EnrollmentsResponse{})
	}))
	defer server.Close()

	client := createTestClient(server.URL, "test-key")

	_, err := client.QueryEnrollments(&EnrollmentFilter{Types: DeviceChannelEnrollmentTypes}, nil)
	require.NoError(t, err)

	require.NotNil(t, capturedFilter)
	assert.Equal(
		t,
		[]string{EnrollmentTypeDevice, EnrollmentTypeUserEnrollmentDevice},
		capturedFilter.Types,
	)
}
