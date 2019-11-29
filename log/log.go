package log

import (
	log "github.com/sirupsen/logrus"
)

func Debug(msg ...interface{}) {
	log.Debug(msg...)
}

func Debugf(format string, msg ...interface{}) {
	log.Debugf(format, msg...)
}

func Info(msg ...interface{}) {
	log.Info(msg...)
}

func Infof(format string, msg ...interface{}) {
	log.Infof(format, msg...)
}

func Warn(msg ...interface{}) {
	log.Warn(msg...)
}

func Warnf(format string, msg ...interface{}) {
	log.Warnf(format, msg...)
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
