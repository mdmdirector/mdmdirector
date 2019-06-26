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
		delay = 10
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
