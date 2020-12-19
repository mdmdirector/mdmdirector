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
	return flag.Lookup("signing-private-key").Value.(flag.Getter).Get().(string)
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

func DBUsername() string {
	return flag.Lookup("db-username").Value.(flag.Getter).Get().(string)
}

func DBPassword() string {
	return flag.Lookup("db-password").Value.(flag.Getter).Get().(string)
}

func DBName() string {
	return flag.Lookup("db-name").Value.(flag.Getter).Get().(string)
}

func DBHost() string {
	return flag.Lookup("db-host").Value.(flag.Getter).Get().(string)
}

func DBPort() string {
	return flag.Lookup("db-port").Value.(flag.Getter).Get().(string)
}

func DBSSLMode() string {
	return flag.Lookup("db-sslmode").Value.(flag.Getter).Get().(string)
}

func EscrowURL() string {
	return flag.Lookup("escrowurl").Value.(flag.Getter).Get().(string)
}

func LogLevel() string {
	return flag.Lookup("loglevel").Value.(flag.Getter).Get().(string)
}

func ClearDeviceOnEnroll() bool {
	return flag.Lookup("clear-device-on-enroll").Value.(flag.Getter).Get().(bool)
}

func ScepCertIssuer() string {
	return flag.Lookup("scep-cert-issuer").Value.(flag.Getter).Get().(string)
}

func ScepCertMinValidity() int {
	return flag.Lookup("scep-cert-min-validity").Value.(flag.Getter).Get().(int)
}

func EnrollmentProfile() string {
	return flag.Lookup("enrollment-profile").Value.(flag.Getter).Get().(string)
}

func SignedEnrollmentProfile() bool {
	return flag.Lookup("enrollment-profile-signed").Value.(flag.Getter).Get().(bool)
}
