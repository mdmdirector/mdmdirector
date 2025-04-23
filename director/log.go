package director

import (
	log "github.com/sirupsen/logrus"
)

type LogHolder struct {
	DeviceUDID         string
	DeviceSerial       string
	CommandUUID        string
	CommandRequestType string
	CommandStatus      string
	ProfileUUID        string
	ProfileIdentifier  string
	Message            string
	Metric             string
}

// func init() {
// 	LogLevel, err := log.ParseLevel(utils.LogLevel())
// 	if err != nil {
// 		log.Fatalf("Unable to parse the log level - %s \n", err)
// 	}
// 	log.SetLevel(LogLevel)
// }

func processFields(logholder LogHolder) *log.Entry {
	logger := log.WithFields(log.Fields{})
	if logholder.DeviceUDID != "" {
		logger = logger.WithFields(
			log.Fields{
				"device_udid": logholder.DeviceUDID,
			})
	}

	if logholder.DeviceSerial != "" {
		logger = logger.WithFields(
			log.Fields{
				"device_serial": logholder.DeviceSerial,
			})
	}

	if logholder.CommandUUID != "" {
		logger = logger.WithFields(
			log.Fields{
				"command_uuid": logholder.CommandUUID,
			})
	}

	if logholder.CommandStatus != "" {
		logger = logger.WithFields(
			log.Fields{
				"command_status": logholder.CommandStatus,
			})
	}

	if logholder.CommandRequestType != "" {
		logger = logger.WithFields(
			log.Fields{
				"command_request_type": logholder.CommandRequestType,
			})
	}

	if logholder.ProfileUUID != "" {
		logger = logger.WithFields(
			log.Fields{
				"profile_uuid": logholder.ProfileUUID,
			})
	}

	if logholder.ProfileIdentifier != "" {
		logger = logger.WithFields(
			log.Fields{
				"profile_identifier": logholder.ProfileIdentifier,
			})
	}

	if logholder.Metric != "" {
		logger = logger.WithFields(
			log.Fields{
				"metric": logholder.Metric,
			})
	}

	return logger
}

func DebugLogger(logholder LogHolder) {
	logger := processFields(logholder)
	logger.Debug(logholder.Message)
}

func InfoLogger(logholder LogHolder) {
	logger := processFields(logholder)
	logger.Info(logholder.Message)
}

func WarnLogger(logholder LogHolder) {
	logger := processFields(logholder)

	logger.Warn(logholder.Message)
}

func ErrorLogger(logholder LogHolder) {
	logger := processFields(logholder)

	logger.Error(logholder.Message)
}

func FatalLogger(logholder LogHolder) {
	logger := processFields(logholder)

	logger.Fatal(logholder.Message)
}

// Leave the below in in case we ever stop checking each struct field individually
// type hook struct{}

// func (h *hook) Levels() []logrus.Level {
// 	return logrus.AllLevels
// }

// func (h *hook) Fire(e *logrus.Entry) error {
// 	// Remove empty fields
// 	for k, v := range e.Data {
// 		if s, ok := v.(string); ok {
// 			if s == "" {
// 				delete(e.Data, k)
// 				continue
// 			}
// 		}
// 	}
// 	return nil
// }
