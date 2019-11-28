package director

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/jinzhu/gorm"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func SendCommand(commandPayload types.CommandPayload) (types.Command, error) {
	var command types.Command
	var commandResponse types.CommandResponse
	jsonStr, err := json.Marshal(commandPayload)
	if err != nil {
		return command, err
	}
	req, _ := http.NewRequest("POST", utils.ServerURL()+"/v1/commands", bytes.NewBuffer(jsonStr))

	req.SetBasicAuth("micromdm", utils.APIKey())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return command, err
	}

	err = json.NewDecoder(resp.Body).Decode(&commandResponse)

	if err != nil {
		return command, err
	}

	defer resp.Body.Close()

	command.DeviceUDID = commandPayload.UDID
	command.CommandUUID = commandResponse.Payload.CommandUUID
	command.RequestType = commandPayload.RequestType

	db.DB.Create(&command)
	return command, nil
}

func UpdateCommand(ackEvent *types.AcknowledgeEvent, device types.Device) error {
	var command types.Command

	if device.UDID == "" {
		log.Errorf("Cannot update command %v without a device UDID!!!!", ackEvent.CommandUUID)
	}

	log.Debug(ackEvent)

	if err := db.DB.Where("device_ud_id = ? AND command_uuid = ?", device.UDID, ackEvent.CommandUUID).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			return errors.New("Command not found in the queue")
		}
	} else {
		if ackEvent.Status == "Error" {
			log.Info("Error response receieved: %v", ackEvent)
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

func InstallAppInQueue(device types.Device, data string) (bool, error) {
	var commandModel types.Command

	err := db.DB.Model(&commandModel).Where("device_ud_id = ? AND request_type = ? AND data = ?", device.UDID, "InstallApplication", data).Where("status = ? OR status = ?", "", "NotNow").First(&commandModel).Error
	if err != nil {
		if gorm.IsRecordNotFoundError(err) {
			return false, nil
		}
		return false, errors.Wrap(err, "Install App in queue")
	}

	return true, nil
}

func ClearCommands(device *types.Device) error {
	var command types.Command
	var commands []types.Command
	log.Infof("Clearing command queue for %v", device.UDID)
	err := db.DB.Model(&command).Where("device_ud_id = ?", device.UDID).Not("status = ? OR status = ?", "Error", "Acknowledged").Delete(&commands).Error
	if err != nil {
		return errors.Wrapf(err, "Failed to clear Command Queue for %v", device.UDID)
	}

	return nil
}

func GetAllCommands(w http.ResponseWriter, r *http.Request) {
	var commands []types.Command

	err := db.DB.Find(&commands).Scan(&commands).Error
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

func DeletePendingCommands(w http.ResponseWriter, r *http.Request) {
	var commands []types.Command

	err := db.DB.Find(&commands).Where("status = ? OR status = ?", "", "NotNow").Scan(&commands).Delete(&commands).Error
	if err != nil {
		log.Errorf("Couldn't scan to Commands model: %v", err)
	}
	// output, err := json.MarshalIndent(&commands, "", "    ")
	// if err != nil {
	// 	log.Error(err)
	// 	w.WriteHeader(http.StatusInternalServerError)
	// }

	// w.Write(output)
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
