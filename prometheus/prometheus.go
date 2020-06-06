package prometheus

import (
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/log"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/prometheus/client_golang/prometheus"
)

func Metrics() {

	var devices []types.Device
	var count float64
	totalDevices := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "micromdm",
		Subsystem: "devices",
		Name:      "count",
		Help:      "Total number of devices in MDMDirector",
	})
	// register totalDevices
	prometheus.MustRegister(totalDevices)
	// loop over the ticker and update the total devices every 10 seconds
	go func() {
		for range time.Tick(time.Second * 10) {
			err := db.DB.Find(&devices).Count(&count).Error
			if err != nil {
				log.Error(err)
			}
			totalDevices.Set(count)
		}
	}()
}
