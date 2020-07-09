package director

import (
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
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
		Name:      "sharedprofiles",
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

	var installedsharedprofiles []types.SharedProfile
	var installedprofilescount float64
	totalInstalledSharedProfiles := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "micromdm",
		Subsystem: "profiles",
		Name:      "installedsharedprofilescount",
		Help:      "Total number of installed shared profiles in MDMDirector",
	})
	// register totalInstalledSharedProfiles
	prometheus.MustRegister(totalInstalledSharedProfiles)
	// loop over the ticker and update the total devices every 10 seconds
	go func() {
		for range time.Tick(time.Second * 10) {
			err := db.DB.Find(&installedsharedprofiles).Where("installed = ?", true).Count(&installedprofilescount).Error
			if err != nil {
				log.Error(err)
			}
			totalInstalledSharedProfiles.Set(installedprofilescount)
		}
	}()

	var uninstalledsharedprofiles []types.SharedProfile
	var uninstalledprofilescount float64
	totalUninstalledSharedProfiles := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "micromdm",
		Subsystem: "profiles",
		Name:      "uninstalledsharedprofilescount",
		Help:      "Total number of uninstalled shared profiles in MDMDirector",
	})
	// register totalUninstalledSharedProfiles
	prometheus.MustRegister(totalUninstalledSharedProfiles)
	// loop over the ticker and update the total devices every 10 seconds
	go func() {
		for range time.Tick(time.Second * 10) {
			err := db.DB.Find(&uninstalledsharedprofiles).Where("installed = ?", false).Count(&uninstalledprofilescount).Error
			if err != nil {
				log.Error(err)
			}
			totalUninstalledSharedProfiles.Set(uninstalledprofilescount)
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

	var installeddeviceprofiles []types.DeviceProfile
	var installeddeviceprofilescount float64
	totalInstalledDeviceProfiles := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "micromdm",
		Subsystem: "profiles",
		Name:      "installeddeviceprofilescount",
		Help:      "Total number of installed device profiles in MDMDirector",
	})
	// register totalInstalledDeviceProfiles
	prometheus.MustRegister(totalInstalledDeviceProfiles)
	// loop over the ticker and update the total devices every 10 seconds
	go func() {
		for range time.Tick(time.Second * 10) {
			err := db.DB.Find(&installeddeviceprofiles).Where("installed = ?", true).Count(&installeddeviceprofilescount).Error
			if err != nil {
				log.Error(err)
			}
			totalInstalledDeviceProfiles.Set(installeddeviceprofilescount)
		}
	}()

	var uninstalleddeviceprofiles []types.DeviceProfile
	var uninstalleddeviceprofilescount float64
	totalUninstalledDeviceProfiles := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "micromdm",
		Subsystem: "profiles",
		Name:      "uninstalleddeviceprofilescount",
		Help:      "Total number of uninstalled device profiles in MDMDirector",
	})
	// register totalUninstalledDeviceProfiles
	prometheus.MustRegister(totalUninstalledDeviceProfiles)
	// loop over the ticker and update the total devices every 10 seconds
	go func() {
		for range time.Tick(time.Second * 10) {
			err := db.DB.Find(&uninstalleddeviceprofiles).Where("installed = ?", false).Count(&uninstalleddeviceprofilescount).Error
			if err != nil {
				log.Error(err)
			}
			totalUninstalledSharedProfiles.Set(uninstalleddeviceprofilescount)
		}
	}()
}
