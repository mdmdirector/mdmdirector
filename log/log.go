package log

import (
	"github.com/grahamgilbert/mdmdirector/utils"
	log "github.com/sirupsen/logrus"
)

func Debug(msg ...interface{}) {
	level := utils.LogLevel()
	if level == "debug" || level == "info" || level == "warn" || level == "error" {
		log.Debug(msg...)
	}
}

func Debugf(format string, msg ...interface{}) {
	level := utils.LogLevel()
	if level == "debug" || level == "info" || level == "warn" || level == "error" {
		log.Debugf(format, msg...)
	}
}

func Info(msg ...interface{}) {
	level := utils.LogLevel()
	if level == "info" || level == "warn" || level == "error" {
		log.Info(msg...)
	}
}

func Infof(format string, msg ...interface{}) {
	level := utils.LogLevel()
	if level == "info" || level == "warn" || level == "error" {
		log.Infof(format, msg...)
	}
}

func Warn(msg ...interface{}) {
	level := utils.LogLevel()
	if level == "warn" || level == "error" {
		log.Warn(msg...)
	}
}

func Warnf(format string, msg ...interface{}) {
	level := utils.LogLevel()
	if level == "warn" || level == "error" {
		log.Warnf(format, msg...)
	}
}

func Error(msg ...interface{}) {
	log.Error(msg...)
}

func Errorf(format string, msg ...interface{}) {
	log.Errorf(format, msg...)
}

func Fatal(msg ...interface{}) {
	log.Fatal(msg...)
}

func Fatalf(format string, msg ...interface{}) {
	log.Fatalf(format, msg...)
}
