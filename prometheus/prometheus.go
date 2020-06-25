package prometheus

import (
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/log"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/prometheus/client_golang/prometheus"
)

func Metrics() {
	totalDevices()
	profiles()
}

func totalDevices() {
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

func profiles() {
	var sharedprofiles []types.SharedProfile
	var count float64
	totalSharedProfiles := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "micromdm",
		Subsystem: "profiles",
		Name:      "sharedprofilescount",
		Help:      "Total number of shared profiles in MDMDirector",
	})
	// register totalSharedProfiles
	prometheus.MustRegister(totalSharedProfiles)
	// loop over the ticker and update the total devices every 10 seconds
	go func() {
		for range time.Tick(time.Second * 10) {
			err := db.DB.Find(&sharedprofiles).Count(&count).Error
			if err != nil {
				log.Error(err)
			}
			totalSharedProfiles.Set(count)
		}
	}()

	var deviceprofiles []types.DeviceProfile
	var deviceprofilescount float64
	totalDeviceProfiles := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "micromdm",
		Subsystem: "profiles",
		Name:      "deviceprofilescount",
		Help:      "Total number of device profiles in MDMDirector",
	})
	// register totalDeviceProfiles
	prometheus.MustRegister(totalDeviceProfiles)
	// loop over the ticker and update the total devices every 10 seconds
	go func() {
		for range time.Tick(time.Second * 10) {
			err := db.DB.Find(&deviceprofiles).Count(&deviceprofilescount).Error
			if err != nil {
				log.Error(err)
			}
			totalDeviceProfiles.Set(deviceprofilescount)
		}
	}()
}
