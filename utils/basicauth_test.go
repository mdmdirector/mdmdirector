package utils

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBasicAuth(t *testing.T) {
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	username := "testuser"
	password := "testpass"

	authHandler := BasicAuth(mockHandler)
	req := httptest.NewRequest("GET", "/", nil)

	// Test unauthenticated request
	rr := httptest.NewRecorder()
	authHandler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusUnauthorized {
		t.Errorf("Expected status code %d, got %d", http.StatusUnauthorized, status)
	}

	// Test authenticated request
	req.SetBasicAuth(username, password)
	rr = httptest.NewRecorder()
	authHandler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, status)
	}
}

func TestValidateUsernameAndPassword(t *testing.T) {
	testCases := []struct {
		requestUsername, requestPassword, desiredUsername, desiredPassword string
		expectedResult                                                     bool
	}{
		{"testuser", "testpass", "testuser", "testpass", false},
		{"testuser", "wrongpass", "testuser", "testpass", true},
		{"wronguser", "testpass", "testuser", "testpass", true},
		{"wronguser", "wrongpass", "testuser", "testpass", true},
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
