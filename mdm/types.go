package mdm

// EnrollmentStatus is the per-enrollment ID results of Push or Enqueue APIs
type EnrollmentStatus struct {
	PushError    string `json:"push_error,omitempty"`
	PushResult   string `json:"push_result,omitempty"`
	CommandError string `json:"command_error,omitempty"`
}

// APIResponse is the result of Push or Enqueue APIs
type APIResponse struct {
	// Status is the per-enrollment ID results of push or enqueue APIs.
	// Map key is the enrollment ID.
	Status map[string]EnrollmentStatus `json:"status,omitempty"`

	// NoPush signifies if APNs pushes were not enabled for this API call.
	NoPush bool `json:"no_push,omitempty"`

	// PushError is present if there was an error sending the APNs push notifications.
	PushError string `json:"push_error,omitempty"`

	// CommandError is present if there was an error enqueuing the command.
	CommandError string `json:"command_error,omitempty"`

	CommandUUID string `json:"command_uuid,omitempty"` // CommandUUID of the enqueued command.
	RequestType string `json:"request_type,omitempty"` // RequestType of the enqueued command.
}

// HasErrors returns true if any enrollment had errors
func (r *APIResponse) HasErrors() bool {
	if r.PushError != "" || r.CommandError != "" {
		return true
	}
	for _, status := range r.Status {
		if status.PushError != "" || status.CommandError != "" {
			return true
		}
	}
	return false
}

// ErrorsForID returns push and command errors for a specific enrollment ID
func (r *APIResponse) ErrorsForID(id string) (pushErr, cmdErr string) {
	if status, ok := r.Status[id]; ok {
		return status.PushError, status.CommandError
	}
	return "", ""
}

// MDMCommandPlist is the plist structure nanoMDM expects for /enqueue
type MDMCommandPlist struct {
	Command     MDMCommand `plist:"Command"`
	CommandUUID string     `plist:"CommandUUID"`
}

// MDMCommand represents the inner command dict in the plist
type MDMCommand struct {
	RequestType string   `plist:"RequestType"`
	Identifier  string   `plist:"Identifier,omitempty"`
	ManifestURL string   `plist:"ManifestURL,omitempty"`
	Queries     []string `plist:"Queries,omitempty"`
	Payload     []byte   `plist:"Payload,omitempty"`
	PIN         string   `plist:"PIN,omitempty"`
}

// EnqueueOptions configures the enqueue request
type EnqueueOptions struct {
	NoPush bool // Skip sending APNs push notification
}

// QueueCommand represents a command in the queue
type QueueCommand struct {
	CommandUUID string `json:"command_uuid"`
	RequestType string `json:"request_type"`
	Command     string `json:"command"` // base64-encoded plist
}

// QueueResponse is the response from queue inspect API
type QueueResponse struct {
	Commands   []QueueCommand `json:"commands,omitempty"`
	NextCursor string         `json:"next_cursor,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// QueueDeleteResponse is the response from queue delete API
type QueueDeleteResponse struct {
	Status map[string]string `json:"status,omitempty"` // enrollment_id -> error message
	Error  string            `json:"error,omitempty"`
}

// EnrollmentDevice contains device info within an enrollment (device channel)
type EnrollmentDevice struct {
	SerialNumber  string  `json:"serial_number,omitempty"`
	DeviceCertPEM *string `json:"device_cert,omitempty"`
	UnlockToken   *string `json:"unlock_token,omitempty"`
}

// EnrollmentUser contains user info within an enrollment (user channel)
type EnrollmentUser struct {
	ShortName string `json:"user_short_name,omitempty"`
	LongName  string `json:"user_long_name,omitempty"`
}

// Enrollment represents an enrollment from NanoMDM
type Enrollment struct {
	ID               string            `json:"id"`
	Type             string            `json:"type,omitempty"` // Device, User, User Enrollment (Device), User Enrollment, Shared iPad
	Device           *EnrollmentDevice `json:"device,omitempty"`
	User             *EnrollmentUser   `json:"user,omitempty"`
	Enabled          bool              `json:"enabled"`
	TokenUpdateTally int               `json:"token_update_tally,omitempty"`
	LastSeen         string            `json:"last_seen,omitempty"`
}

// EnrollmentFilter is the filter for querying enrollments
type EnrollmentFilter struct {
	IDs            []string `json:"ids,omitempty"`
	Serials        []string `json:"serials,omitempty"`
	UserShortNames []string `json:"user_short_names,omitempty"`
	Types          []string `json:"types,omitempty"`
	Enabled        *bool    `json:"enabled,omitempty"`
}

// EnrollmentQueryOptions configures the enrollment query
type EnrollmentQueryOptions struct {
	IncludeDeviceCert  bool `json:"include_device_cert,omitempty"`
	IncludeUnlockToken bool `json:"include_unlock_token,omitempty"`
}

// EnrollmentQueryRequest is the request body for enrollment query
type EnrollmentQueryRequest struct {
	Filter  *EnrollmentFilter       `json:"filter,omitempty"`
	Options *EnrollmentQueryOptions `json:"options,omitempty"`
}

// EnrollmentsResponse is the response from enrollments query API
type EnrollmentsResponse struct {
	Enrollments []Enrollment `json:"enrollments,omitempty"`
	NextCursor  string       `json:"next_cursor,omitempty"`
	Error       string       `json:"error,omitempty"`
}

// UnifiedQueueCommand represents a command in the queue
type UnifiedQueueCommand struct {
	UUID    string `json:"uuid"`
	Payload string `json:"payload"` // Base64-encoded plist
}

// UnifiedQueueResponse is the unified response format for queue inspection (microMDM-compatible)
type UnifiedQueueResponse struct {
	Commands []UnifiedQueueCommand `json:"commands"`
}
