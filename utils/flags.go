package utils

import (
	"flag"
	"strings"
)

func ServerURL() string {
	return strings.TrimRight(flag.Lookup("micromdmurl").Value.(flag.Getter).Get().(string), "/")
}

func ApiKey() string {
	return flag.Lookup("micromdmapikey").Value.(flag.Getter).Get().(string)
}

func DebugMode() bool {
	return flag.Lookup("debug").Value.(flag.Getter).Get().(bool)
}
