package mdm

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"sync"

	"github.com/pkg/errors"
)

// PaginationConfig - configuration for paginated requests
type PaginationConfig struct {
	PageSize        int // Number of items per page
	MaxParallelReqs int // Maximum parallel requests
}

// DefaultPaginationConfig - defaults for pagination
func DefaultPaginationConfig() PaginationConfig {
	return PaginationConfig{
		PageSize:        500,
		MaxParallelReqs: 5,
	}
}

// queryEnrollmentsPage fetches a single page of enrollments using offset-based pagination
func (c *NanoMDMClient) queryEnrollmentsPage(filter *EnrollmentFilter, limit, offset int) (*EnrollmentsResponse, error) {
	u, err := url.Parse(c.serverURL)
	if err != nil {
		return nil, errors.Wrap(err, "queryEnrollmentsPage: parse server URL")
	}
	u.Path = path.Join(u.Path, "v1", "enrollments", "query")

	q := u.Query()
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset >= 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	u.RawQuery = q.Encode()

	reqBody := EnrollmentQueryRequest{
		Filter: filter,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, errors.Wrap(err, "queryEnrollmentsPage: marshal request")
	}

	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, errors.Wrap(err, "queryEnrollmentsPage: create request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(NanoMDMAuthUsername, c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "queryEnrollmentsPage: execute request")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "queryEnrollmentsPage: read response")
	}

	var result EnrollmentsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, errors.Wrapf(err, "queryEnrollmentsPage: decode response: %s", string(body))
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = "unexpected status code"
		}
		return &result, errors.Errorf("queryEnrollmentsPage: %s (status %d)", errMsg, resp.StatusCode)
	}

	return &result, nil
}

// pageResult result from a single page fetch
type pageResult struct {
	offset      int
	enrollments []Enrollment
	err         error
}

// getEnrollmentsParallel fetches enrollments with filter in parallel based on pagination config
func (c *NanoMDMClient) getEnrollmentsParallel(filter *EnrollmentFilter, config *PaginationConfig) (*EnrollmentsResponse, error) {
	if config == nil {
		defaultCfg := DefaultPaginationConfig()
		config = &defaultCfg
	}
	if config.PageSize <= 0 {
		config.PageSize = 500
	}
	if config.MaxParallelReqs <= 0 {
		config.MaxParallelReqs = 5
	}

	firstPage, err := c.queryEnrollmentsPage(filter, config.PageSize, 0)
	if err != nil {
		return nil, errors.Wrap(err, "GetEnrollmentsParallel: fetch first page")
	}

	if len(firstPage.Enrollments) < config.PageSize {
		return firstPage, nil
	}

	// fetching batches of pages in parallel until we get a page with fewer results
	allEnrollments := append([]Enrollment{}, firstPage.Enrollments...)

	currentOffset := config.PageSize
	done := false

	for !done {
		var offsets []int
		for i := 0; i < config.MaxParallelReqs; i++ {
			offsets = append(offsets, currentOffset+(i*config.PageSize))
		}

		results := make(chan pageResult, len(offsets))
		var wg sync.WaitGroup

		for _, offset := range offsets {
			wg.Add(1)
			go func(off int) {
				defer wg.Done()
				resp, err := c.queryEnrollmentsPage(filter, config.PageSize, off)
				if err != nil {
					results <- pageResult{offset: off, err: err}
					return
				}
				results <- pageResult{offset: off, enrollments: resp.Enrollments}
			}(offset)
		}

		go func() {
			wg.Wait()
			close(results)
		}()

		pageResults := make(map[int]pageResult)
		for result := range results {
			pageResults[result.offset] = result
		}

		for _, offset := range offsets {
			result, ok := pageResults[offset]
			if !ok {
				continue
			}

			if result.err != nil {
				return nil, errors.Wrapf(result.err, "GetEnrollmentsParallel: fetch page at offset %d", offset)
			}

			if len(result.enrollments) == 0 {
				done = true
				break
			}

			allEnrollments = append(allEnrollments, result.enrollments...)

			if len(result.enrollments) < config.PageSize {
				done = true
				break
			}
		}

		currentOffset += config.MaxParallelReqs * config.PageSize
	}

	return &EnrollmentsResponse{
		Enrollments: allEnrollments,
	}, nil
}

// QueryEnrollments fetches all enrollments matching the filter
func (c *NanoMDMClient) QueryEnrollments(filter *EnrollmentFilter, config *PaginationConfig) (*EnrollmentsResponse, error) {
	return c.getEnrollmentsParallel(filter, config)
}

// GetAllEnrollments fetches all enrollments
func (c *NanoMDMClient) GetAllEnrollments(config *PaginationConfig) (*EnrollmentsResponse, error) {
	return c.getEnrollmentsParallel(nil, config)
}
