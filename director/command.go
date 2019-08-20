package director

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/grahamgilbert/mdmdirector/db"
	"github.com/grahamgilbert/mdmdirector/log"
	"github.com/grahamgilbert/mdmdirector/types"
	"github.com/grahamgilbert/mdmdirector/utils"
	"github.com/jinzhu/gorm"
)

func SendCommand(CommandPayload types.CommandPayload) (*types.Command, error) {
	var command types.Command
	var commandResponse types.CommandResponse
	jsonStr, err := json.Marshal(CommandPayload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", utils.ServerURL()+"/v1/commands", bytes.NewBuffer(jsonStr))

	req.SetBasicAuth("micromdm", utils.ApiKey())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	err = json.NewDecoder(resp.Body).Decode(&commandResponse)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if CommandPayload.RequestType == "InstallApplication" {
		command.Data = CommandPayload.ManifestURL
	}

	command.DeviceUDID = CommandPayload.UDID
	command.CommandUUID = commandResponse.Payload.CommandUUID
	command.RequestType = CommandPayload.RequestType

	db.DB.Create(&command)
	return &command, nil
}

func UpdateCommand(ackEvent *types.AcknowledgeEvent, device types.Device) error {
	var command types.Command

	if err := db.DB.Where("device_ud_id = ? AND command_uuid = ?", device.UDID, ackEvent.CommandUUID).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			return errors.New("Command not found in the queue")
		}
	} else {
		if ackEvent.Status == "Error" {
			err := db.DB.Model(&command).Where("device_ud_id = ? AND command_uuid = ?", device.UDID, ackEvent.CommandUUID).Updates(types.Command{
				Status:      ackEvent.Status,
				ErrorString: string(ackEvent.RawPayload),
			}).Error
			if err != nil {
				return err
			}
		} else {
			err := db.DB.Model(&command).Where("device_ud_id = ? AND command_uuid = ?", device.UDID, ackEvent.CommandUUID).Updates(types.Command{
				Status:      ackEvent.Status,
				ErrorString: "",
			}).Error
			if err != nil {
				return err
			}
		}

	}
	return nil
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

func InstallAppInQueue(device types.Device, data string) bool {
	var commandModel types.Command

	err := db.DB.Model(&commandModel).Where("device_ud_id = ? AND request_type = ? AND data = ?", device.UDID, "InstallApplication", data).Where("status = ? OR status = ?", "", "NotNow").First(&commandModel).Error
	if err != nil {
		if gorm.IsRecordNotFoundError(err) {
			return false
		}
	}

	return true

}

func ClearCommands(device *types.Device) error {
	var command types.Command
	var commands []types.Command
	log.Infof("Clearing command queue for %v", device.UDID)
	err := db.DB.Model(&command).Where("device_ud_id = ?", device.UDID).Where("status = ? OR status = ?", "", "NotNow").Delete(&commands).Error
	if err != nil {
		return err
	}

	return nil
}

func GetPendingCommands(w http.ResponseWriter, r *http.Request) {
	var commands []types.Command

	err := db.DB.Find(&commands).Where("status = ? OR status = ?", "", "NotNow").Scan(&commands).Error
	if err != nil {
		log.Errorf("Couldn't scan to Commands model: %v", err)
	}
	output, err := json.MarshalIndent(&commands, "", "    ")
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	w.Write(output)

}

func GetErrorCommands(w http.ResponseWriter, r *http.Request) {
	var commands []types.Command

	err := db.DB.Find(&commands).Where("status = ?", "Error").Scan(&commands).Error
	if err != nil {
		log.Errorf("Couldn't scan to Commands model: %v", err)
	}
	output, err := json.MarshalIndent(&commands, "", "    ")
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	w.Write(output)

}
