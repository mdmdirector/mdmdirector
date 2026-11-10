package types

type PostPayload struct {
	Topic            string            `json:"topic"`
	EventID          string            `json:"event_id"`
	CheckinEvent     *CheckinEvent     `json:"checkin_event,omitempty"`
	AcknowledgeEvent *AcknowledgeEvent `json:"acknowledge_event,omitempty"`
}

type CheckinEvent struct {
	UDID string `json:"udid"`
	// EnrollmentID is the MDM server's authoritative enrollment ID for the event.
	// A user-channel enrollment ID is "<deviceUDID>:<userID>", so only its first
	// colon-delimited segment identifies the device.
	EnrollmentID string            `json:"enrollment_id"`
	Params       map[string]string `json:"url_params"`
	RawPayload   []byte            `json:"raw_payload"`
}

type AcknowledgeEvent struct {
	UDID string `json:"udid"`
	// EnrollmentID is the MDM server's authoritative enrollment ID for the event.
	// See CheckinEvent.EnrollmentID.
	EnrollmentID string            `json:"enrollment_id"`
	Params       map[string]string `json:"url_params"`
	RawPayload   []byte            `json:"raw_payload"`
	CommandUUID  string            `json:"command_uuid"`
	Status       string            `json:"status"`
}
