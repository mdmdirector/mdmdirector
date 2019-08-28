package log

import (
	"github.com/grahamgilbert/mdmdirector/utils"
	log "github.com/sirupsen/logrus"
)

func Debug(msg ...interface{}) {
	level := utils.LogLevel()
	if level == "debug" {
		log.Debug(msg...)
	}
}

func Debugf(format string, msg ...interface{}) {
	level := utils.LogLevel()
	if level == "debug" {
		log.Debugf(format, msg...)
	}
}

func Info(msg ...interface{}) {
	level := utils.LogLevel()
	if level == "debug" || level == "info" {
		log.Info(msg...)
	}
}

func Infof(format string, msg ...interface{}) {
	level := utils.LogLevel()
	if level == "debug" || level == "info" {
		log.Infof(format, msg...)
	}
}

func Warn(msg ...interface{}) {
	level := utils.LogLevel()
	if level == "debug" || level == "info" || level == "warn" {
		log.Warn(msg...)
	}
}

func Warnf(format string, msg ...interface{}) {
	level := utils.LogLevel()
	if level == "debug" || level == "info" || level == "warn" {
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
