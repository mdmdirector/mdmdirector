package main

import (
	"flag"
	"fmt"
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

const BasicAuthUser = "mdmdirector"

func main() {
	var port string
	var debugMode bool
	flag.BoolVar(&debugMode, "debug", false, "Enable debug output")
	flag.BoolVar(&PushNewBuild, "push-new-build", false, "Re-push profiles if the device's build number changes.")
	flag.StringVar(&port, "port", "8000", "Port number to run mdmdirector on.")
	flag.StringVar(&MicroMDMURL, "micromdmurl", "", "MicroMDM Server URL")
	flag.StringVar(&MicroMDMAPIKey, "micromdmapikey", "", "MicroMDM Server API Key")
	flag.BoolVar(&Sign, "sign", false, "Sign profiles prior to sending to MicroMDM.")
	flag.StringVar(&KeyPassword, "password", "", "Password to encrypt/read the signing key(optional) or p12 file.")
	flag.StringVar(&KeyPath, "private-key", "", "Path to the signing private key. Don't use with p12 file.")
	flag.StringVar(&CertPath, "cert", "", "Path to the signing certificate or p12 file.")

	flag.Parse()

	if MicroMDMURL == "" {
		log.Fatal("MicroMDM Server URL missing. Exiting.")
	}

	if MicroMDMAPIKey == "" {
		log.Fatal("MicroMDM API Key missing. Exiting.")
	}

	r := mux.NewRouter()
	r.HandleFunc("/webhook", director.WebhookHandler).Methods("POST")
	r.HandleFunc("/profile", director.PostProfileHandler).Methods("POST")
	r.HandleFunc("/profile", director.DeleteProfileHandler).Methods("DELETE")
	r.HandleFunc("/profile/{udid}", director.GetDeviceProfiles).Methods("GET")
	r.HandleFunc("/device", utils.BasicAuth(director.DeviceHandler, BasicAuthUser, "123456", "Please enter your username and password for this site")).Methods("GET")
	r.HandleFunc("/installapplication", director.PostInstallApplicationHandler).Methods("POST")
	http.Handle("/", r)

	if err := db.Open(); err != nil {
		log.Fatal("Failed to open database")
	}
	defer db.Close()

	db.DB.LogMode(debugMode)

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

	fmt.Println("mdmdirector is running, hold onto your butts...")

	go director.RetryCommands()
	go director.ScheduledCheckin()
	go director.FetchDevicesFromMDM()

	log.Print(http.ListenAndServe(":"+port, nil))
}
