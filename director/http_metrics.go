package director

import (
	"net/http"

	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/utils"
)

// statusResponseWriter records the first status code a handler writes. The /profile
// handlers have many error branches, several of which write an error status and keep
// going, so the first status written is the request's real outcome.
type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusResponseWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// WithProfileAPIMetrics counts requests to the /profile API by method and result.
// method is "post" or "delete".
func WithProfileAPIMetrics(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !utils.Prometheus() {
			next(w, r)
			return
		}

		recorder := &statusResponseWriter{ResponseWriter: w}
		next(recorder, r)

		// A handler that returns without writing anything has implicitly succeeded
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		metrics.ProfileAPIRequests(method, metrics.ResultLabel(status)).Inc()
	}
}
