package mdm

import (
	"net/http"

	"github.com/pkg/errors"
)

// ConvertToUnifiedResponse converts a nanoMDM QueueResponse to UnifiedQueueResponse (microMDM response)
func ConvertToUnifiedResponse(nanoResp *QueueResponse) (*UnifiedQueueResponse, error) {
	unified := &UnifiedQueueResponse{
		Commands: make([]UnifiedQueueCommand, 0, len(nanoResp.Commands)),
	}

	for _, cmd := range nanoResp.Commands {
		unified.Commands = append(unified.Commands, UnifiedQueueCommand{
			UUID:    cmd.CommandUUID,
			Payload: cmd.Command,
		})
	}

	return unified, nil
}

// InspectQueue retrieves queued MDM commands for a specific device ID
func (c *NanoMDMClient) InspectQueue(enrollmentID string) (*QueueResponse, error) {
	endpoint, err := c.buildURL("queue", []string{enrollmentID}, nil)
	if err != nil {
		return nil, errors.Wrap(err, "InspectQueue")
	}

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.Wrap(err, "InspectQueue: create request")
	}

	result, status, err := doRequest[QueueResponse](c, req)
	if err != nil {
		return result, errors.Wrap(err, "InspectQueue")
	}

	if status != http.StatusOK {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = "unexpected status code"
		}
		return result, errors.Errorf("InspectQueue: %s (status %d)", errMsg, status)
	}

	return result, nil
}

// ClearQueue clears all queued MDM commands for one or more device IDs
func (c *NanoMDMClient) ClearQueue(enrollmentIDs ...string) (*QueueDeleteResponse, error) {
	if len(enrollmentIDs) == 0 {
		return nil, errors.New("no enrollment IDs provided")
	}

	endpoint, err := c.buildURL("queue", enrollmentIDs, nil)
	if err != nil {
		return nil, errors.Wrap(err, "ClearQueue")
	}

	req, err := http.NewRequest(http.MethodDelete, endpoint, nil)
	if err != nil {
		return nil, errors.Wrap(err, "ClearQueue: create request")
	}

	result, status, err := doRequest[QueueDeleteResponse](c, req)
	if err != nil {
		return nil, errors.Wrap(err, "ClearQueue")
	}

	if status == http.StatusInternalServerError {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = "all operations failed"
		}
		return result, errors.New(errMsg)
	}

	// 204 = empty queue (doRequest returns new(T)), 207 = partial success — both returned without error
	return result, nil
}
