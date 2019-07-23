package director

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"

	"github.com/grahamgilbert/mdmdirector/db"
	"github.com/grahamgilbert/mdmdirector/types"
	"github.com/grahamgilbert/mdmdirector/utils"
	"github.com/jinzhu/gorm"
)

func SendCommand(CommandPayload types.CommandPayload) (*types.Command, error) {
	var command types.Command
	var commandResponse types.CommandResponse
	jsonStr, err := json.Marshal(CommandPayload)
	if err != nil {
		log.Print(err)
		return nil, err
	}
	req, err := http.NewRequest("POST", utils.ServerURL()+"/v1/commands", bytes.NewBuffer(jsonStr))

	req.SetBasicAuth("micromdm", utils.ApiKey())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Print(err)
		return nil, err
	}

	err = json.NewDecoder(resp.Body).Decode(&commandResponse)

	if err != nil {
		log.Print("No decoding for you")
		log.Print(err)
		return nil, err
	}

	defer resp.Body.Close()

	command.DeviceUDID = CommandPayload.UDID
	command.CommandUUID = commandResponse.Payload.CommandUUID
	command.RequestType = CommandPayload.RequestType

	db.DB.Create(&command)
	return &command, nil
}

func UpdateCommand(ackEvent *types.AcknowledgeEvent, device types.Device) {
	var command types.Command
	if err := db.DB.Where("device_ud_id = ? AND command_uuid = ?", device.UDID, ackEvent.CommandUUID).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			log.Print("Command not found in the queue")
			return
		}
	} else {

		err := db.DB.Model(&command).Where("device_ud_id = ? AND command_uuid = ?", device.UDID, ackEvent.CommandUUID).Updates(types.Command{
			Status: ackEvent.Status,
		}).Error
		if err != nil {
			log.Print(err)
		}
	}
}

func CommandInQueue(device types.Device, command string) bool {
	var commandModel types.Command

	err := db.DB.Model(&commandModel).Where("device_ud_id = ? AND request_type = ?", device.UDID, command).Where("status = ? OR status = ?", "", "NotNow").First(&commandModel).Error
	if err != nil {
		if gorm.IsRecordNotFoundError(err) {
			return false
		}
	}

	return true

}
