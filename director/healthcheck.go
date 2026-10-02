package director

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/mdmdirector/mdmdirector/db"
)

const healthCheckTimeout = 5 * time.Second

// pinger is satisfied by the *sql.DB pool backing gorm.
type pinger interface {
	PingContext(ctx context.Context) error
}

// resolvePinger hands back the pool that real traffic uses
var resolvePinger = func() (pinger, error) {
	if db.DB == nil {
		return nil, errors.New("database is not open")
	}
	return db.DB.DB()
}

// HealthCheck reports whether mdmdirector can reach its database. It pings the
// connection pool directly instead of issuing a query
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancel()

	if err := pingDB(ctx); err != nil {
		ErrorLogger(LogHolder{Message: "health check failed: database unavailable: " + err.Error()})
		writeHealth(w, http.StatusServiceUnavailable, `{"status":"DOWN"}`)
		return
	}

	writeHealth(w, http.StatusOK, `{"status":"UP"}`)
}

func pingDB(ctx context.Context) error {
	p, err := resolvePinger()
	if err != nil {
		return err
	}
	return p.PingContext(ctx)
}

func writeHealth(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status is already sent, so a failed write can only be logged.
	if _, err := w.Write([]byte(body)); err != nil {
		ErrorLogger(LogHolder{Message: "couldn't report status: " + err.Error()})
	}
}
