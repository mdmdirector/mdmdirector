package mdm

import (
	"net/http"
	"strconv"

	"github.com/pkg/errors"
)

// queueInspectPageSize is the page size InspectQueue asks NanoMDM for. NanoMDM's queue
// API pages by limit/offset and returns 100 commands when no limit is given, without
// saying whether there are more, so a single unpaged call silently stops at 100.
const queueInspectPageSize = 100

// queueInspectMaxCommands bounds how many commands InspectQueue collects for one device,
// so a runaway queue can't make one inspect request hold an unbounded response in memory.
// Past it, the result is cut off and marked Truncated.
var queueInspectMaxCommands = 5000

// ConvertToUnifiedResponse converts a nanoMDM QueueResponse to UnifiedQueueResponse (microMDM response)
func ConvertToUnifiedResponse(nanoResp *QueueResponse) (*UnifiedQueueResponse, error) {
	unified := &UnifiedQueueResponse{
		Commands:  make([]UnifiedQueueCommand, 0, len(nanoResp.Commands)),
		Total:     len(nanoResp.Commands),
		Truncated: nanoResp.Truncated,
	}

	for _, cmd := range nanoResp.Commands {
		unified.Commands = append(unified.Commands, UnifiedQueueCommand{
			UUID:        cmd.CommandUUID,
			RequestType: cmd.RequestType,
			Payload:     cmd.Command,
		})
	}

	return unified, nil
}

// InspectQueue retrieves the queued MDM commands for a specific device ID, following
// NanoMDM's pages until the queue is exhausted or queueInspectMaxCommands is reached
// (then Truncated is set).
func (c *NanoMDMClient) InspectQueue(enrollmentID string) (*QueueResponse, error) {
	result := &QueueResponse{}
	seen := make(map[string]bool)
	offset := 0
	for {
		// Ask for one past the cap on the last page, so a queue of exactly the cap isn't
		// reported as truncated.
		limit := queueInspectPageSize
		if remaining := queueInspectMaxCommands + 1 - len(result.Commands); remaining < limit {
			limit = remaining
		}

		page, err := c.InspectQueuePage(enrollmentID, limit, offset)
		if err != nil {
			return page, err
		}

		for _, cmd := range page.Commands {
			// The queue can move between pages (a device answering a command shifts the
			// offsets), so the same command may come back twice.
			if seen[cmd.CommandUUID] {
				continue
			}
			seen[cmd.CommandUUID] = true
			result.Commands = append(result.Commands, cmd)
		}

		if len(result.Commands) > queueInspectMaxCommands {
			result.Commands = result.Commands[:queueInspectMaxCommands]
			result.Truncated = true
			return result, nil
		}
		// A short page is the end of the queue. A page longer than asked for means the
		// server ignored the limit and sent everything.
		if len(page.Commands) != limit {
			return result, nil
		}
		offset += len(page.Commands)
	}
}

// InspectQueuePage retrieves one page of queued MDM commands for a specific device ID.
func (c *NanoMDMClient) InspectQueuePage(enrollmentID string, limit, offset int) (*QueueResponse, error) {
	endpoint, err := c.buildURL("queue", []string{enrollmentID}, map[string]string{
		"limit":  strconv.Itoa(limit),
		"offset": strconv.Itoa(offset),
	})
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

	// 204 = empty queue (doRequest returns new(T)), 207 = partial success - both returned without error
	return result, nil
}
