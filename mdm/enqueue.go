package mdm

import (
	"bytes"
	"encoding/base64"
	"net/http"

	"github.com/google/uuid"
	"github.com/groob/plist"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/pkg/errors"
)

// Enqueue sends an MDM command to one or more enrollments
func (c *NanoMDMClient) Enqueue(enrollmentIDs []string, payload types.CommandPayload, opts *EnqueueOptions) (*APIResponse, error) {
	if len(enrollmentIDs) == 0 {
		return nil, errors.New("no enrollment IDs provided")
	}

	// Build the MDM command plist
	cmdUUID := uuid.New().String()
	mdmCmd := MDMCommandPlist{
		CommandUUID: cmdUUID,
		Command: MDMCommand{
			RequestType: payload.RequestType,
			Identifier:  payload.Identifier,
			ManifestURL: payload.ManifestURL,
			Queries:     payload.Queries,
			PIN:         payload.Pin,
		},
	}

	// Callers pass Payload base64-encoded — historically required so it could travel as JSON to micromdm,
	// which then base64-decoded it before building the plist.
	// nanomdm has no such decode step: we need to build the plist here.
	// So, undo the caller's base64 first, and pass plist directly
	if payload.Payload != "" {
		raw, err := base64.StdEncoding.DecodeString(payload.Payload)
		if err != nil {
			return nil, errors.Wrap(err, "Enqueue: decode payload")
		}
		mdmCmd.Command.Payload = raw
	}

	plistData, err := plist.MarshalIndent(mdmCmd, "\t")
	if err != nil {
		return nil, errors.Wrap(err, "Enqueue: marshal plist")
	}

	return c.enqueueRaw(enrollmentIDs, plistData, opts)
}

// enqueueRaw is the internal implementation for enqueue operations
func (c *NanoMDMClient) enqueueRaw(enrollmentIDs []string, plistData []byte, opts *EnqueueOptions) (*APIResponse, error) {
	var queryParams map[string]string
	if opts != nil && opts.NoPush {
		queryParams = map[string]string{"nopush": "1"}
	}

	endpoint, err := c.buildURL("enqueue", enrollmentIDs, queryParams)
	if err != nil {
		return nil, errors.Wrap(err, "Enqueue")
	}

	req, err := http.NewRequest(http.MethodPut, endpoint, bytes.NewBuffer(plistData))
	if err != nil {
		return nil, errors.Wrap(err, "Enqueue: create request")
	}
	req.Header.Set("Content-Type", "application/xml")

	result, status, err := doRequest[APIResponse](c, req)
	if err != nil {
		return result, errors.Wrap(err, "Enqueue")
	}
	if err := apiResponseError(result, status); err != nil {
		return result, errors.Wrap(err, "Enqueue")
	}

	return result, nil
}
