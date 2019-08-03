package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/grahamgilbert/mdmdirector/db"
	"github.com/grahamgilbert/mdmdirector/director"
	"github.com/grahamgilbert/mdmdirector/types"
	"github.com/grahamgilbert/mdmdirector/utils"
)

// MicroMDMURL is the url for your MicroMDM server
var MicroMDMURL string

// MicroMDMAPIKey is the api key for your MicroMDM server
var MicroMDMAPIKey string

//MicroMDMUsername is the username for your MicroMDM server
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

// DBConnectionString is used to connect to the datbase
var DBConnectionString string

func main() {
	var port string
	var debugMode bool
	flag.BoolVar(&debugMode, "debug", false, "Enable debug output")
	flag.BoolVar(&PushNewBuild, "push-new-build", false, "Re-push profiles if the device's build number changes.")
	flag.StringVar(&port, "port", "8000", "Port number to run mdmdirector on.")
	flag.StringVar(&MicroMDMURL, "micromdmurl", "", "MicroMDM Server URL")
	flag.StringVar(&MicroMDMAPIKey, "micromdmapikey", "", "MicroMDM Server API Key")
	flag.BoolVar(&Sign, "sign", false, "Sign profiles prior to sending to MicroMDM.")
	flag.StringVar(&KeyPassword, "key-password", "", "Password to encrypt/read the signing key(optional) or p12 file.")
	flag.StringVar(&KeyPath, "private-key", "", "Path to the signing private key. Don't use with p12 file.")
	flag.StringVar(&CertPath, "cert", "", "Path to the signing certificate or p12 file.")
	flag.StringVar(&BasicAuthPass, "password", "", "Password used for basic authentication")
	flag.StringVar(&DBConnectionString, "dbconnection", "", "Database connection string")
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

	r := mux.NewRouter()
	r.HandleFunc("/webhook", director.WebhookHandler).Methods("POST")
	r.HandleFunc("/profile", utils.BasicAuth(director.PostProfileHandler)).Methods("POST")
	r.HandleFunc("/profile", utils.BasicAuth(director.DeleteProfileHandler)).Methods("DELETE")
	r.HandleFunc("/profile", utils.BasicAuth(director.GetSharedProfiles)).Methods("GET")
	r.HandleFunc("/profile/{udid}", utils.BasicAuth(director.GetDeviceProfiles)).Methods("GET")
	r.HandleFunc("/device", utils.BasicAuth(director.DeviceHandler)).Methods("GET")
	r.HandleFunc("/installapplication", utils.BasicAuth(director.PostInstallApplicationHandler)).Methods("POST")
	r.HandleFunc("/command/pending", utils.BasicAuth(director.GetPendingCommands)).Methods("GET")
	r.HandleFunc("/command/error", utils.BasicAuth(director.GetErrorCommands)).Methods("GET")
	http.Handle("/", r)

	if err := db.Open(); err != nil {
		log.Fatal("Failed to open database")
	}
	defer db.Close()

	// db.DB.LogMode(debugMode)

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
	)

	log.Print("mdmdirector is running, hold onto your butts...")

	go director.ScheduledCheckin()
	go director.FetchDevicesFromMDM()
	go director.RetryCommands()

	log.Print(http.ListenAndServe(":"+port, nil))
}
