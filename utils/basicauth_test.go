package utils

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// Test helper function to create a request with Basic Auth credentials
func createRequestWithBasicAuth(username, password string) *http.Request {
	req := httptest.NewRequest("GET", "http://example.com/foo", nil)
	req.SetBasicAuth(username, password)
	return req
}

// Test helper function to create a handler for testing purposes
func testHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Welcome to the protected area!")
}

func TestBasicAuth(t *testing.T) {

	username := "mdmdirector"
	password := "testpass"
	// Set environment variable to password
	os.Setenv("DIRECTOR_PASSWORD", password)
	// Create a test handler with BasicAuth
	protectedHandler := BasicAuth(testHandler)

	// Test with valid credentials
	req := createRequestWithBasicAuth(username, password)
	rr := httptest.NewRecorder()
	protectedHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			rr.Code, http.StatusOK)
	}

	expected := "Welcome to the protected area!"
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), expected)
	}

	// Test with invalid credentials
	req = createRequestWithBasicAuth("wronguser", "wrongpass")
	rr = httptest.NewRecorder()
	protectedHandler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("handler returned wrong status code: got %v want %v",
			rr.Code, http.StatusUnauthorized)
	}

	expected = "Unauthorised.\n"
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), expected)
	}
}

func TestValidateUsernameAndPassword(t *testing.T) {
	testCases := []struct {
		requestUsername, requestPassword, desiredUsername, desiredPassword string
		expectedResult                                                     bool
	}{
		{"testuser", "testpass", "testuser", "testpass", true},
		{"testuser", "wrongpass", "testuser", "testpass", false},
		{"wronguser", "testpass", "testuser", "testpass", false},
		{"wronguser", "wrongpass", "testuser", "testpass", false},
	}

	for _, tc := range testCases {
		result := validateUsernameAndPassword(
			tc.requestUsername,
			tc.requestPassword,
			tc.desiredUsername,
			tc.desiredPassword,
		)
		if result != tc.expectedResult {
			t.Errorf(
				"Expected result %t, got %t for username %s and password %s",
				tc.expectedResult,
				result,
				tc.requestUsername,
				tc.requestPassword,
			)
		}
	}
}
