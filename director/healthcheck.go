package director

import "net/http"

func HealthCheck(w http.ResponseWriter, r *http.Request) {
	output := []byte("{\"status\":\"UP\"}")
	_, err := w.Write(output)
	if err != nil {
		http.Error(w, "couldn't report status", http.StatusInternalServerError)
		return
	}
}
