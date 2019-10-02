package utils

import (
	"flag"
	"strings"
)

func ServerURL() string {
	return strings.TrimRight(flag.Lookup("micromdmurl").Value.(flag.Getter).Get().(string), "/")
}

func APIKey() string {
	return flag.Lookup("micromdmapikey").Value.(flag.Getter).Get().(string)
}

func DebugMode() bool {
	return flag.Lookup("debug").Value.(flag.Getter).Get().(bool)
}

func Sign() bool {
	return flag.Lookup("sign").Value.(flag.Getter).Get().(bool)
}

func KeyPassword() string {
	return flag.Lookup("key-password").Value.(flag.Getter).Get().(string)
}

func KeyPath() string {
	return flag.Lookup("private-key").Value.(flag.Getter).Get().(string)
}

func CertPath() string {
	return flag.Lookup("cert").Value.(flag.Getter).Get().(string)
}

func PushOnNewBuild() bool {
	return flag.Lookup("push-new-build").Value.(flag.Getter).Get().(bool)
}

func GetBasicAuthUser() string {
	return "mdmdirector"
}

func GetBasicAuthPassword() string {
	return flag.Lookup("password").Value.(flag.Getter).Get().(string)
}

func DBConnectionString() string {
	return flag.Lookup("dbconnection").Value.(flag.Getter).Get().(string)
}

func LogLevel() string {
	return flag.Lookup("loglevel").Value.(flag.Getter).Get().(string)
}
