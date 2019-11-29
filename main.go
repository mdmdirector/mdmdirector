package main

import (
	"flag"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/director"
	"github.com/mdmdirector/mdmdirector/log"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/sirupsen/logrus"
)

// MicroMDMURL is the url for your MicroMDM server
var MicroMDMURL string

// MicroMDMAPIKey is the api key for your MicroMDM server
var MicroMDMAPIKey string

// MicroMDMUsername is the username for your MicroMDM server
var MicroMDMUsername = "micromdm"

// Sign is whether profiles should be signed (they really should)
var Sign bool

// KeyPassword is the password for the private key
var KeyPassword string

// KeyPath is the path for the private key
var KeyPath string

// CertPath is the path for the signing cert or p12 file
var CertPath string

// PushNewBuild is whether to push all profiles if the device's build number changes
var PushNewBuild bool

var BasicAuthUser = utils.GetBasicAuthUser()

// BasicAuthPass is the password used for basic auth
var BasicAuthPass string

// DBConnectionString is used to connect to the database
var DBConnectionString string

// LogLevel = log level
var LogLevel string

func main() {
	var port string
	var debugMode bool
	logrus.SetLevel(logrus.DebugLevel)
	flag.BoolVar(&debugMode, "debug", false, "Enable debug mode")
	flag.BoolVar(&PushNewBuild, "push-new-build", true, "Re-push profiles if the device's build number changes.")
	flag.StringVar(&port, "port", "8000", "Port number to run mdmdirector on.")
	flag.StringVar(&MicroMDMURL, "micromdmurl", "", "MicroMDM Server URL")
	flag.StringVar(&MicroMDMAPIKey, "micromdmapikey", "", "MicroMDM Server API Key")
	flag.BoolVar(&Sign, "sign", false, "Sign profiles prior to sending to MicroMDM.")
	flag.StringVar(&KeyPassword, "key-password", "", "Password to encrypt/read the signing key(optional) or p12 file.")
	flag.StringVar(&KeyPath, "private-key", "", "Path to the signing private key. Don't use with p12 file.")
	flag.StringVar(&CertPath, "cert", "", "Path to the signing certificate or p12 file.")
	flag.StringVar(&BasicAuthPass, "password", "", "Password used for basic authentication")
	flag.StringVar(&DBConnectionString, "dbconnection", "", "Database connection string")
	flag.StringVar(&LogLevel, "loglevel", "warn", "Log level. One of debug, info, warn, error")
	flag.Parse()

	if MicroMDMURL == "" {
		log.Fatal("MicroMDM Server URL missing. Exiting.")
	}

	if MicroMDMAPIKey == "" {
		log.Fatal("MicroMDM API Key missing. Exiting.")
	}

	if BasicAuthPass == "" {
		log.Fatal("Basic Auth password missing. Exiting.")
	}

	if DBConnectionString == "" {
		log.Fatal("Database details missing. Exiting.")
	}

	if LogLevel != "debug" && LogLevel != "info" && LogLevel != "warn" && LogLevel != "error" {
		log.Fatal("loglevel value is not one of debug, info, warn or error.")
	}

	r := mux.NewRouter()
	r.HandleFunc("/webhook", director.WebhookHandler).Methods("POST")
	r.HandleFunc("/profile", utils.BasicAuth(director.PostProfileHandler)).Methods("POST")
	r.HandleFunc("/profile", utils.BasicAuth(director.DeleteProfileHandler)).Methods("DELETE")
	r.HandleFunc("/profile", utils.BasicAuth(director.GetSharedProfiles)).Methods("GET")
	r.HandleFunc("/profile/{udid}", utils.BasicAuth(director.GetDeviceProfiles)).Methods("GET")
	r.HandleFunc("/device", utils.BasicAuth(director.DeviceHandler)).Methods("GET")
	r.HandleFunc("/device/{udid}", utils.BasicAuth(director.SingleDeviceHandler)).Methods("GET")
	r.HandleFunc("/installapplication", utils.BasicAuth(director.PostInstallApplicationHandler)).Methods("POST")
	r.HandleFunc("/installapplication", utils.BasicAuth(director.GetSharedApplicationss)).Methods("GET")
	r.HandleFunc("/command/pending", utils.BasicAuth(director.GetPendingCommands)).Methods("GET")
	r.HandleFunc("/command/pending/delete", utils.BasicAuth(director.DeletePendingCommands)).Methods("GET")
	r.HandleFunc("/command/error", utils.BasicAuth(director.GetErrorCommands)).Methods("GET")
	r.HandleFunc("/command", utils.BasicAuth(director.GetAllCommands)).Methods("GET")
	r.HandleFunc("/health", director.HealthCheck).Methods("GET")
	http.Handle("/", r)

	if err := db.Open(); err != nil {
		log.Fatal("Failed to open database")
	}
	defer db.Close()

	// db.DB.LogMode(true)
	db.DB.Exec("CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";")
	db.DB.AutoMigrate(
		&types.Device{},
		&types.DeviceProfile{},
		&types.Command{},
		&types.SharedProfile{},
		&types.SecurityInfo{},
		&types.FirmwarePasswordStatus{},
		&types.ManagementStatus{},
		&types.OSUpdateSettings{},
		&types.SharedInstallApplication{},
		&types.DeviceInstallApplication{},
		&types.Certificate{},
	)

	log.Info("mdmdirector is running, hold onto your butts...")

	go director.FetchDevicesFromMDM()
	go director.ScheduledCheckin()
	// go director.UnconfiguredDevices()
	// go director.RetryCommands()

	log.Info(http.ListenAndServe(":"+port, nil))
}
