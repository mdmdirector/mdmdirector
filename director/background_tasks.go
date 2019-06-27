package director

import (
	"log"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/grahamgilbert/mdmdirector/db"
	"github.com/grahamgilbert/mdmdirector/types"
	"github.com/grahamgilbert/mdmdirector/utils"
)

func RetryCommands() {
	var delay time.Duration
	if utils.DebugMode() {
		delay = 20
	} else {
		delay = 120
	}
	ticker := time.NewTicker(delay * time.Second)
	defer ticker.Stop()
	fn := func() {
		sendPush()
	}

	fn()

	for {
		select {
		case <-ticker.C:
			fn()
		}
	}
}

func sendPush() {
	var command types.Command
	var commands []types.Command
	err := db.DB.Model(&command).Not("status = ?", "Acknowledged").Scan(&commands).Error
	if err != nil {
		log.Print(err)
	}

	client := &http.Client{}

	for _, queuedCommand := range commands {
		endpoint, err := url.Parse(utils.ServerURL())
		endpoint.Path = path.Join(endpoint.Path, "push", queuedCommand.DeviceUDID)
		req, err := http.NewRequest("GET", endpoint.String(), nil)
		req.SetBasicAuth("micromdm", utils.ApiKey())

		resp, err := client.Do(req)
		if err != nil {
			log.Print(err)
			continue
		}

		resp.Body.Close()
	}
}

func ScheduledCheckin() {
	var delay time.Duration
	if utils.DebugMode() {
		delay = 60
	} else {
		delay = 7200
	}
	ticker := time.NewTicker(delay * time.Second)
	defer ticker.Stop()
	fn := func() {
		processCheckin()
	}

	fn()

	for {
		select {
		case <-ticker.C:
			fn()
		}
	}
}

func processCheckin() {
	var devices []types.Device
	var device types.Device
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	err := db.DB.Model(&device).Where("updated_at < ? AND active = ?", twoHoursAgo, true).Scan(&devices).Error
	if err != nil {
		log.Print(err)
	}

	for _, staleDevice := range devices {
		var commandPayload types.CommandPayload
		commandPayload.UDID = staleDevice.UDID
		commandPayload.RequestType = "ProfileList"

		SendCommand(commandPayload)

	}
}
