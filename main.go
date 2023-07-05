package main

import (
	"flag"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/mdmdirector/mdmdirector/db"
	"github.com/mdmdirector/mdmdirector/director"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/vmihailenco/taskq/v3"
	"github.com/vmihailenco/taskq/v3/redisq"

	"github.com/micromdm/go4/env"
	log "github.com/sirupsen/logrus"
)

// MicroMDMURL is the url for your MicroMDM server
var MicroMDMURL string

// MicroMDMAPIKey is the api key for your MicroMDM server
var MicroMDMAPIKey string

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

// BasicAuthPass is the password used for basic auth
var BasicAuthPass string

// DBUsername is the account to connect to the database
var DBUsername string

// DBPassword is used to connect to the database
var DBPassword string

// DBName is used to connect to the database
var DBName string

// DBHost is used to connect to the database
var DBHost string

// DBPort is used to connect to the database
var DBPort string

var DBMaxConnections int

// DBSSLMode is used to connect to the database
var DBSSLMode string

// LogLevel = log level
var LogLevel string

// EscrowURL = url to escrow erase and lock PINs to
var EscrowURL string

var ClearDeviceOnEnroll bool

var ScepCertIssuer string

var ScepCertMinValidity int

var EnrollmentProfile string

var SignEnrollmentProfile bool

var LogFormat string

var Prometheus bool

var RedisHost string

var RedisPort string

var RedisPassword string

var OnceIn int

var InfoRequestInterval int

func main() {
	var port string
	var debugMode bool
	flag.BoolVar(&debugMode, "debug", env.Bool("DEBUG", false), "Enable debug mode")
	flag.BoolVar(
		&PushNewBuild,
		"push-new-build",
		env.Bool("PUSH_NEW_BUILD", true),
		"Re-push profiles if the device's build number changes.",
	)
	flag.StringVar(
		&port,
		"port",
		env.String("DIRECTOR_PORT", "8000"),
		"Port number to run mdmdirector on.",
	)
	flag.StringVar(&MicroMDMURL, "micromdmurl", env.String("MICRO_URL", ""), "MicroMDM Server URL")
	flag.StringVar(
		&MicroMDMAPIKey,
		"micromdmapikey",
		env.String("MICRO_API_KEY", ""),
		"MicroMDM Server API Key",
	)
	flag.BoolVar(
		&Sign,
		"sign",
		env.Bool("SIGN", false),
		"Sign profiles prior to sending to MicroMDM.",
	)
	flag.StringVar(
		&KeyPassword,
		"key-password",
		env.String("SIGNING_PASSWORD", ""),
		"Password to encrypt/read the signing key(optional) or p12 file.",
	)
	flag.StringVar(
		&KeyPath,
		"signing-private-key",
		env.String("SIGNING_KEY", ""),
		"Path to the signing private key. Don't use with p12 file.",
	)
	flag.StringVar(
		&CertPath,
		"cert",
		env.String("SIGNING_CERT", ""),
		"Path to the signing certificate or p12 file.",
	)
	flag.StringVar(
		&BasicAuthPass,
		"password",
		env.String("DIRECTOR_PASSWORD", ""),
		"Password used for basic authentication",
	)
	flag.StringVar(
		&DBUsername,
		"db-username",
		env.String("DB_USERNAME", ""),
		"The username associated with the Postgres instance",
	)
	flag.StringVar(
		&DBPassword,
		"db-password",
		env.String("DB_PASSWORD", ""),
		"The password of the db user account",
	)
	flag.StringVar(
		&DBName,
		"db-name",
		env.String("DB_NAME", ""),
		"The name of the Postgres database to use",
	)
	flag.StringVar(
		&DBHost,
		"db-host",
		env.String("DB_HOST", ""),
		"The hostname or IP of the Postgres instance",
	)
	flag.StringVar(
		&DBPort,
		"db-port",
		env.String("DB_PORT", "5432"),
		"The port of the Postgres instance",
	)
	flag.StringVar(
		&RedisHost,
		"redis-host",
		env.String("REDIS_HOST", "localhost"),
		"Redis hostname",
	)
	flag.StringVar(&RedisPort, "redis-port", env.String("REDIS_PORT", "6379"), "Redis port")
	flag.StringVar(
		&RedisPassword,
		"redis-password",
		env.String("REDIS_PASSWORD", ""),
		"Redis password",
	)
	flag.StringVar(
		&DBSSLMode,
		"db-sslmode",
		"disable",
		"The SSL Mode to use to connect to Postgres",
	)
	flag.IntVar(
		&DBMaxConnections,
		"db-max-connections",
		100,
		"Maximum number of database connections",
	)
	flag.StringVar(
		&LogLevel,
		"loglevel",
		env.String("LOG_LEVEL", "warn"),
		"Log level. One of debug, info, warn, error",
	)
	flag.StringVar(
		&EscrowURL,
		"escrowurl",
		env.String("ESCROW_URL", ""),
		"HTTP endpoint to escrow erase and unlock PINs to.",
	)
	flag.BoolVar(
		&ClearDeviceOnEnroll,
		"clear-device-on-enroll",
		env.Bool("CLEAR_DEVICE_ON_ENROLL", false),
		"Deletes device profiles and install applications when a device enrolls",
	)
	flag.StringVar(
		&LogFormat,
		"log-format",
		env.String("LOG_FORMAT", "logfmt"),
		"Format to output logs. Defaults to logfmt. Can be set to logfmt or json.",
	)
	flag.StringVar(
		&ScepCertIssuer,
		"scep-cert-issuer",
		env.String("SCEP_CERT_ISSUER", "OU=MICROMDM SCEP CA,O=MicroMDM,C=US"),
		"The issuer of your SCEP certificate",
	)
	flag.IntVar(
		&ScepCertMinValidity,
		"scep-cert-min-validity",
		env.Int("SCEP_CERT_MIN_VALIDITY", 180),
		"The number of days at which the SCEP certificate has remaining before the enrollment profile is re-sent.",
	)
	flag.StringVar(
		&EnrollmentProfile,
		"enrollment-profile",
		env.String("ENROLLMENT_PROFILE", ""),
		"Path to enrollment profile.",
	)
	flag.BoolVar(
		&SignEnrollmentProfile,
		"enrollment-profile-signed",
		env.Bool("ENROLMENT_PROFILE_SIGNED", false),
		"Is the enrollment profile you are providing already signed",
	)
	flag.BoolVar(&Prometheus, "prometheus", env.Bool("PROMETHEUS", false), "Enable Prometheus")
	flag.IntVar(
		&OnceIn,
		"once-in",
		env.Int("ONCE_IN", 60),
		"Number of minutes to wait before queuing an additional command for any device which already has commands queued. Defaults to 60. Ignored and overidden as 2 (minutes) if --debug is passed.",
	)
	flag.IntVar(
		&InfoRequestInterval,
		"info-request-interval",
		env.Int("INFO_REQUEST_INTERVAL", 360),
		"Number of minutes to wait between issuing information commands",
	)
	flag.Parse()

	logLevel, err := log.ParseLevel(LogLevel)
	if err != nil {
		log.Fatalf("Unable to parse the log level - %s \n", err)
	}
	log.SetLevel(logLevel)

	if LogFormat == "json" {
		log.SetFormatter(&log.JSONFormatter{
			DisableHTMLEscape: true,
		})
	} else {
		log.SetFormatter(&log.TextFormatter{
			FullTimestamp: true,
			DisableQuote:  true,
		})
	}

	if MicroMDMURL == "" {
		log.Fatal("MicroMDM Server URL missing. Exiting.")
	}

	if MicroMDMAPIKey == "" {
		log.Fatal("MicroMDM API Key missing. Exiting.")
	}

	if BasicAuthPass == "" {
		log.Fatal("Basic Auth password missing. Exiting.")
	}

	if DBUsername == "" || DBPassword == "" || DBName == "" || DBHost == "" || DBPort == "" ||
		DBSSLMode == "" {
		log.Fatal("Required database details missing, Exiting.")
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
	r.HandleFunc("/device/command/{command}", utils.BasicAuth(director.PostDeviceCommandHandler)).
		Methods("POST")
	r.HandleFunc("/device/serial/{serial}", utils.BasicAuth(director.SingleDeviceSerialHandler)).
		Methods("GET")
	r.HandleFunc("/device/push/{udid}", utils.BasicAuth(director.PushDeviceHandler)).Methods("GET")
	r.HandleFunc("/device/{udid}", utils.BasicAuth(director.SingleDeviceHandler)).Methods("GET")
	r.HandleFunc("/installapplication", utils.BasicAuth(director.PostInstallApplicationHandler)).
		Methods("POST")
	r.HandleFunc("/installapplication", utils.BasicAuth(director.GetSharedApplicationss)).
		Methods("GET")
	r.HandleFunc("/command/pending", utils.BasicAuth(director.GetPendingCommands)).Methods("GET")
	r.HandleFunc("/command/pending/delete", utils.BasicAuth(director.DeletePendingCommands)).
		Methods("GET")
	r.HandleFunc("/command/error", utils.BasicAuth(director.GetErrorCommands)).Methods("GET")
	r.HandleFunc("/command", utils.BasicAuth(director.GetAllCommands)).Methods("GET")
	r.HandleFunc("/health", director.HealthCheck).Methods("GET")

	director.InfoLogger(director.LogHolder{Message: "Connecting to database"})
	if err := db.Open(); err != nil {
		director.ErrorLogger(director.LogHolder{Message: err.Error()})
		log.Fatal("Failed to open database")
	}
	director.InfoLogger(director.LogHolder{Message: "Connected to database"})

	director.InfoLogger(director.LogHolder{Message: "Performing DB migrations if required"})

	err = db.DB.AutoMigrate(
		&types.Device{},
		&types.DeviceProfile{},
		&types.Command{},
		&types.SharedProfile{},
		&types.SecurityInfo{},
		&types.FirmwarePasswordStatus{},
		&types.ManagementStatus{},
		&types.OSUpdateSettings{},
		&types.FirewallSettings{},
		// &types.FirewallSettingsApplication{},
		&types.SecureBoot{},
		&types.SecureBootReducedSecurity{},
		&types.SharedInstallApplication{},
		&types.DeviceInstallApplication{},
		&types.Certificate{},
		&types.ProfileList{},
		&types.UnlockPin{},
	)
	if err != nil {
		director.ErrorLogger(director.LogHolder{Message: err.Error()})
		log.Fatal(err)
	}

	director.InfoLogger(
		director.LogHolder{Message: "mdmdirector is running, hold onto your butts..."},
	)

	var QueueFactory = redisq.NewFactory()

	var PushQueue = QueueFactory.RegisterQueue(&taskq.QueueOptions{
		Name:  "pushnotifications",
		Redis: director.RedisClient(), // go-redis client
	})
	err = PushQueue.Purge()
	if err != nil {
		log.Error(err)
	}

	if utils.Prometheus() {
		director.Metrics()
		r.Handle("/metrics", promhttp.Handler())
	}

	go director.FetchDevicesFromMDM()

	// Override OnceIn if --debug is passed
	if debugMode {
		OnceIn = 2
	}

	onceInDuration := (time.Minute * time.Duration(OnceIn))
	go director.ScheduledCheckin(PushQueue, onceInDuration)
	go director.ProcessScheduledCheckinQueue(PushQueue)

	log.Info(http.ListenAndServe(":"+port, r))
}
