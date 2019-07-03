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
)

// MicroMDMURL is the url for your MicroMDM server
var MicroMDMURL string

// MicroMDMAPIKey is the api key for your MicroMDM server
var MicroMDMAPIKey string

//MicroMDMUsername is the username for your MicroMDM server
var MicroMDMUsername = "micromdm"

func main() {
	var port string
	var debugMode bool
	flag.BoolVar(&debugMode, "debug", false, "Enable debug output")
	flag.StringVar(&port, "port", "8000", "Port number to run mdmdirector on.")
	flag.StringVar(&MicroMDMURL, "micromdmurl", "", "MicroMDM Server URL")
	flag.StringVar(&MicroMDMAPIKey, "micromdmapikey", "", "MicroMDM Server API Key")

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
	r.HandleFunc("/device", director.DeviceHandler).Methods("GET")
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
	)

	fmt.Println("mdmdirector is running, hold onto your butts...")

	go director.RetryCommands()
	go director.ScheduledCheckin()
	go director.FetchDevicesFromMDM()

	log.Print(http.ListenAndServe(":"+port, nil))
}
