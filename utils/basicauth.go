package utils

import (
	"crypto/subtle"
	"net/http"

	log "github.com/sirupsen/logrus"
)

// BasicAuth provides basic authentication for certain routes
func BasicAuth(handler http.HandlerFunc) http.HandlerFunc {
	username := GetBasicAuthUser()
	password := GetBasicAuthPassword()

	realm := "Please enter your username and password for this site"
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()

		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`"`)
			w.WriteHeader(401)
			log.Error("Unauthorised request")
			_, _ = w.Write([]byte("Unauthorised.\n"))
			return
		}

		handler(w, r)
	}
}
