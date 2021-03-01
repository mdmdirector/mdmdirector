# MDMDirector

MDMDirector is an opinionated orchestrator for MicroMDM. It enables profiles to be managed with MicroMDM in a stateful manner, via a RESTful API. It also allows for installation of packages either just at enrollment or immediately. It uses MicroMDM's webhook functionality to recieve events from MicroMDM and then instructs MicroMDM to perform appropriate action. As such, MDMDirector does not need to be exposed to the public internet.

## Usage

MDMDirector is a compiled binary - it has no external dependencies other than a Postgresql database, Redis and optionally a signing certificate for signing profiles. It is configured using flags.

### MicroMDM Setup

You must set the `-command-webhook-url` flag on MicroMDM to be the URL that your MDMDirector instance is served on (with the additon of `/webhook`)

```
-command-webhook-url=https://mdmdirector.company.com/webhook
```

### Flags

- `-cert /path/to/certificate` - Path to the signing certificate or p12 file.
- `-clear-device-on-enroll` - Deletes device profiles and install applications when a device enrolls (default "false")
- `-db-host string` - (Required) Hostname or IP of the postgres instance
- `-db-max-connections int` - Maximum number of database connections (default 100)
- `-db-name string` - (Required) Name of the database to connect to.
- `-db-password string` - (Required) Password of the DB user.
- `-db-port string` - The port of the postgres instance (default 5432)
- `-db-sslmode` - The SSL Mode to use to connect to postgres (default "disable")
- `-db-username string` - (Required) Username used to connect to the postgres instance.
- `-redis-host string` - Hostname of your Redis instance (default "localhost").
- `-redis-port string` - Port of your Redis instance (default 6379).
- `-redis-password string` - Password for your redist instance (default is no password).
- `-debug` - Enable debug mode. Does things like shorten intervals for scheduled tasks. Only to be used during development.
- `-enrollment-profile` - Path to enrollment profile.
- `-enrollment-profile-signed` - Is the enrollment profile you are providing already signed (default: false)
- `-escrowurl` - HTTP endpoint to escrow erase and unlock PINs to ([Crypt](https://github.com/grahamgilbert/crypt-server) and other compatible servers).
- `-key-password string` - Password to decrypt the signing key or p12 file.
- `-loglevel string` - Log level. One of debug, info, warn, error (default "warn")
- `-logformat-format` - Log format. Either `logfmt` (the default) or `json`
- `-micromdmapikey string` - (Required) MicroMDM Server API Key
- `-micromdmurl string` - (Required) MicroMDM Server URL
- `-password string` - (Required) Password used for basic authentication
- `-port string` - Port number to run mdmdirector on. (default "8000")
- `-push-new-build` - Re-push profiles if the device's build number changes. (default true)
- `-scep-cert-issuer` - The issuer of your SCEP certificate (default: "CN=MicroMDM,OU=MICROMDM SCEP CA,O=MicroMDM,C=US")
- `-scep-cert-min-validity` - The number of days at which the SCEP certificate has remaining before the enrollment profile is re-sent. (default: 180)
- `-sign` - Sign profiles prior to sending to MicroMDM. Requires `-cert` to be passed.
- `-signing-private-key string` - Path to the signing private key. Don't use with p12 file.
- `-once-in` - Number of minutes to wait before queuing an additional command for any device which already has commands queued. Defaults to 60. Ignored and overidden as 2 (minutes) if --debug is passed.

## Todo

### Documentation

- Posting / removing profiles and apps

### App

- App state inspection binary (perhaps a separate service to MDMDirector due to requiring exposure to the public internet)

## Contributing

- File issues
- Open Pull Requests
