package director

import (
	"time"

	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/types"
)

const gaugePollInterval = time.Minute

// PollGauges starts background pollers that populate - mdmdirector_devices_total,
// mdmdirector_profiles_total and mdmdirector_ddm_enabled_devices_total
func PollGauges() {
	go pollDevices()
	go pollProfiles()
	go pollDDMOptIns()
}

func pollDevices() {
	for range time.Tick(gaugePollInterval) {
		var count int64
		if err := db.DB.Model(&types.Device{}).Count(&count).Error; err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			continue
		}
		metrics.DevicesTotal().Set(float64(count))
	}
}

func pollProfiles() {
	for range time.Tick(gaugePollInterval) {
		setProfilesGauge("shared", &types.SharedProfile{})
		setProfilesGauge("device", &types.DeviceProfile{})
	}
}

// pollDDMOptIns tracks how far the per-device DDM rollout has progressed
func pollDDMOptIns() {
	for range time.Tick(gaugePollInterval) {
		var count int64
		if err := db.DB.Model(&DDMOptIn{}).Count(&count).Error; err != nil {
			ErrorLogger(LogHolder{Message: err.Error()})
			continue
		}
		metrics.DDMEnabledDevices().Set(float64(count))
	}
}

func setProfilesGauge(scope string, model interface{}) {
	var installed, uninstalled int64
	if err := db.DB.Model(model).Where("installed = ?", true).Count(&installed).Error; err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		return
	}
	if err := db.DB.Model(model).Where("installed = ?", false).Count(&uninstalled).Error; err != nil {
		ErrorLogger(LogHolder{Message: err.Error()})
		return
	}
	metrics.ProfilesTotal(scope, "true").Set(float64(installed))
	metrics.ProfilesTotal(scope, "false").Set(float64(uninstalled))
}
