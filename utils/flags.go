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

func DBMaxConnections() int {
	return flag.Lookup("db-max-connections").Value.(flag.Getter).Get().(int)
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

func Prometheus() bool {
	return flag.Lookup("prometheus").Value.(flag.Getter).Get().(bool)
}

func RedisHost() string {
	return flag.Lookup("redis-host").Value.(flag.Getter).Get().(string)
}

func RedisPort() string {
	return flag.Lookup("redis-port").Value.(flag.Getter).Get().(string)
}

func RedisPassword() string {
	return flag.Lookup("redis-password").Value.(flag.Getter).Get().(string)
}

func OnceIn() int {
	return flag.Lookup("once-in").Value.(flag.Getter).Get().(int)
}

func InfoRequestInterval() int {
	return flag.Lookup("info-request-interval").Value.(flag.Getter).Get().(int)
}

// Code for testing goes down here
// flags *can* be overwritten by using os.Args, but they cannot be parsed more than once or it results in a crash.
// So, instead we inject an interface layer between the calling code that is swapped out during unit tests.

// IFlagProvider is the public interface for all flag.Lookup calls
type IFlagProvider interface {
	ClearDeviceOnEnroll() bool
}

// DefaultProvider is the "production" implementation of IFlagProvider. It is simply a skeleton wrapper for all of
// the other legacy public methods in this package.
type DefaultProvider struct{}

// ClearDeviceOnEnroll returns whether to delete device profiles and install applications when a device enrolls
func (provider DefaultProvider) ClearDeviceOnEnroll() bool {
	return ClearDeviceOnEnroll()
}

// FlagProvider is the variable that should be used to access public methods in this file. It intentionally has
// an identical public interface to the legacy public methods in this class to make migration easier.
// During unit tests, simply swap out this public variable for a unit-test-specific implementation of IFlagProvider.
// Swap back the original FlagProvider by simply instantiating a new DefaultProvider.
var FlagProvider IFlagProvider = DefaultProvider{}
