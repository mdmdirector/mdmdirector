package mdm

import (
	"encoding/json"
	"io"
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
	req.SetBasicAuth(NanoMDMAuthUsername, c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "InspectQueue: execute request")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "InspectQueue: read response")
	}

	var result QueueResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, errors.Wrapf(err, "InspectQueue: decode response: %s", string(body))
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = "unexpected status code"
		}
		return &result, errors.Errorf("InspectQueue: %s (status %d)", errMsg, resp.StatusCode)
	}

	return &result, nil
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
	req.SetBasicAuth(NanoMDMAuthUsername, c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "ClearQueue: execute request")
	}
	defer resp.Body.Close()

	// 204 means success with no content
	if resp.StatusCode == http.StatusNoContent {
		return &QueueDeleteResponse{}, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "ClearQueue: read response")
	}

	var result QueueDeleteResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, errors.Wrapf(err, "ClearQueue: decode response: %s", string(body))
	}

	// 500 means all operations failed
	if resp.StatusCode == http.StatusInternalServerError {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = "all operations failed"
		}
		return &result, errors.New(errMsg)
	}

	// 207 means partial success - return result without error so caller can inspect Status
	return &result, nil
}
