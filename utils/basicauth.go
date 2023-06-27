package utils

import (
	"net/http"

	log "github.com/sirupsen/logrus"
)

// BasicAuth provides basic authentication for certain routes
func BasicAuth(handler http.HandlerFunc) http.HandlerFunc {
	username := GetBasicAuthUser()
	password := GetBasicAuthPassword()

	return basicAuthHandler(handler, username, password)
}

func basicAuthHandler(handler http.HandlerFunc, username, password string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		realm := "Please enter your username and password for this site"
		if !ok || !validateUsernameAndPassword(user, pass, username, password) {
			w.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`"`)
			w.WriteHeader(401)
			log.Error("Unauthorised request")
			_, _ = w.Write([]byte("Unauthorised.\n"))
			return
		}

		handler(w, r)
	}
}

func validateUsernameAndPassword(
	requestUsername, requestPassword, desiredUsername, desiredPassword string,
) bool {
	if requestUsername == desiredUsername && requestPassword == desiredPassword {
		return true
	}
	return false
}
