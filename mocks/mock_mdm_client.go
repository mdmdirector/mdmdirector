package mocks

import (
	"github.com/mdmdirector/mdmdirector/mdm"
	"github.com/mdmdirector/mdmdirector/types"
)

// MockMDMClient - mock implementation of MDMClient for testing
type MockMDMClient struct {
	PushFunc              func(enrollmentIDs ...string) (*mdm.APIResponse, error)
	EnqueueFunc           func(enrollmentIDs []string, payload types.CommandPayload, opts *mdm.EnqueueOptions) (*mdm.APIResponse, error)
	InspectQueueFunc      func(enrollmentID string) (*mdm.QueueResponse, error)
	ClearQueueFunc        func(enrollmentIDs ...string) (*mdm.QueueDeleteResponse, error)
	QueryEnrollmentsFunc  func(filter *mdm.EnrollmentFilter, config *mdm.PaginationConfig) (*mdm.EnrollmentsResponse, error)
	GetAllEnrollmentsFunc func(config *mdm.PaginationConfig) (*mdm.EnrollmentsResponse, error)

	// Call tracking
	PushCalls             [][]string
	EnqueueCalls          []EnqueueCall
	InspectQueueCalls     []string
	ClearQueueCalls       [][]string
	QueryEnrollmentCalls  []QueryEnrollmentCall
	GetAllEnrollmentCalls []*mdm.PaginationConfig
}

// EnqueueCall records the arguments passed to Enqueue
type EnqueueCall struct {
	EnrollmentIDs []string
	Payload       types.CommandPayload
	Opts          *mdm.EnqueueOptions
}

// QueryEnrollmentCall records the arguments passed to QueryEnrollments
type QueryEnrollmentCall struct {
	Filter *mdm.EnrollmentFilter
	Config *mdm.PaginationConfig
}

// Ensure MockMDMClient implements MDMClient
var _ mdm.MDMClient = (*MockMDMClient)(nil)

// Push implements MDMClient.Push
func (m *MockMDMClient) Push(enrollmentIDs ...string) (*mdm.APIResponse, error) {
	m.PushCalls = append(m.PushCalls, enrollmentIDs)
	if m.PushFunc != nil {
		return m.PushFunc(enrollmentIDs...)
	}
	return &mdm.APIResponse{}, nil
}

// Enqueue implements MDMClient.Enqueue
func (m *MockMDMClient) Enqueue(enrollmentIDs []string, payload types.CommandPayload, opts *mdm.EnqueueOptions) (*mdm.APIResponse, error) {
	m.EnqueueCalls = append(m.EnqueueCalls, EnqueueCall{
		EnrollmentIDs: enrollmentIDs,
		Payload:       payload,
		Opts:          opts,
	})
	if m.EnqueueFunc != nil {
		return m.EnqueueFunc(enrollmentIDs, payload, opts)
	}
	return &mdm.APIResponse{}, nil
}

// InspectQueue implements MDMClient.InspectQueue
func (m *MockMDMClient) InspectQueue(enrollmentID string) (*mdm.QueueResponse, error) {
	m.InspectQueueCalls = append(m.InspectQueueCalls, enrollmentID)
	if m.InspectQueueFunc != nil {
		return m.InspectQueueFunc(enrollmentID)
	}
	return &mdm.QueueResponse{}, nil
}

// ClearQueue implements MDMClient.ClearQueue
func (m *MockMDMClient) ClearQueue(enrollmentIDs ...string) (*mdm.QueueDeleteResponse, error) {
	m.ClearQueueCalls = append(m.ClearQueueCalls, enrollmentIDs)
	if m.ClearQueueFunc != nil {
		return m.ClearQueueFunc(enrollmentIDs...)
	}
	return &mdm.QueueDeleteResponse{}, nil
}

// QueryEnrollments implements MDMClient.QueryEnrollments
func (m *MockMDMClient) QueryEnrollments(filter *mdm.EnrollmentFilter, config *mdm.PaginationConfig) (*mdm.EnrollmentsResponse, error) {
	m.QueryEnrollmentCalls = append(m.QueryEnrollmentCalls, QueryEnrollmentCall{
		Filter: filter,
		Config: config,
	})
	if m.QueryEnrollmentsFunc != nil {
		return m.QueryEnrollmentsFunc(filter, config)
	}
	return &mdm.EnrollmentsResponse{}, nil
}

// GetAllEnrollments implements MDMClient.GetAllEnrollments
func (m *MockMDMClient) GetAllEnrollments(config *mdm.PaginationConfig) (*mdm.EnrollmentsResponse, error) {
	m.GetAllEnrollmentCalls = append(m.GetAllEnrollmentCalls, config)
	if m.GetAllEnrollmentsFunc != nil {
		return m.GetAllEnrollmentsFunc(config)
	}
	return &mdm.EnrollmentsResponse{}, nil
}

// Reset clears all call tracking
func (m *MockMDMClient) Reset() {
	m.PushCalls = nil
	m.EnqueueCalls = nil
	m.InspectQueueCalls = nil
	m.ClearQueueCalls = nil
	m.QueryEnrollmentCalls = nil
	m.GetAllEnrollmentCalls = nil
}
