package director

import (
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/log"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	TotalPushes = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "micromdm",
		Subsystem: "apns_pushes",
		Name:      "total",
		Help:      "Total number of APNS Pushes completed.",
	})

	ProfilesPushed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "micromdm",
		Subsystem: "profiles",
		Name:      "pushed_total",
		Help:      "Number of profiles pushed.",
	})

	InstallApplicationsPushed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "micromdm",
		Subsystem: "install_applications",
		Name:      "pushed_total",
		Help:      "Number of InstallApplications pushed.",
	})

	TotalPushes60s               float64
	ProfilesPushed60s            float64
	InstallApplicationsPushed60s float64
)

func Metrics() {
	totalDevices()
	profiles()
	resetCounters()
	prometheus.MustRegister(TotalPushes)
	prometheus.MustRegister(ProfilesPushed)
	prometheus.MustRegister(InstallApplicationsPushed)
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

func resetCounters() {
	apnsPushesLast60s := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "micromdm",
		Subsystem: "apns_pushes",
		Name:      "last60s",
		Help:      "Number of APNS pushes in the last minute",
	})
	// register apnsPushesLast60s
	prometheus.MustRegister(apnsPushesLast60s)

	profilePushesLast60s := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "micromdm",
		Subsystem: "profiles",
		Name:      "last60s",
		Help:      "Number of Profiles Pushed in the last minute",
	})
	// register profilePushesLast60s
	prometheus.MustRegister(profilePushesLast60s)

	installApplicationPushesLast60s := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "micromdm",
		Subsystem: "install_applications",
		Name:      "last60s",
		Help:      "Number of InstallApplications Pushed in the last minute",
	})
	// register installApplicationPushesLast60s
	prometheus.MustRegister(installApplicationPushesLast60s)
	go func() {
		for range time.Tick(time.Second * 60) {
			apnsPushesLast60s.Set(TotalPushes60s)
			TotalPushes60s = 0
			profilePushesLast60s.Set(ProfilesPushed60s)
			ProfilesPushed60s = 0
			installApplicationPushesLast60s.Set(InstallApplicationsPushed60s)
			InstallApplicationsPushed60s = 0
		}
	}()
}
