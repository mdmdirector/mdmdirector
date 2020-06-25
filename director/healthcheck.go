package director

import "net/http"

// todo
// verify database is accessible
// verify we can connect to micromdm
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	output := []byte("{\"status\":\"UP\"}")
	_, err := w.Write(output)
	if err != nil {
		http.Error(w, "couldn't report status", http.StatusInternalServerError)
		return
	}
}
