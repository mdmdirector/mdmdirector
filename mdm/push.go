package mdm

import (
	"net/http"

	"github.com/pkg/errors"
)

// Push sends APNs push notifications to one or more enrollments
func (c *NanoMDMClient) Push(enrollmentIDs ...string) (*APIResponse, error) {
	if len(enrollmentIDs) == 0 {
		return nil, errors.New("no enrollment IDs provided")
	}

	endpoint, err := c.buildURL("push", enrollmentIDs, nil)
	if err != nil {
		return nil, errors.Wrap(err, "Push")
	}

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.Wrap(err, "Push: create request")
	}

	result, err := c.doRequest(req)
	if err != nil {
		return result, errors.Wrap(err, "Push")
	}

	return result, nil
}
