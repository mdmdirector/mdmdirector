# MDMDirector

MDMDirector is an opinionated orchestrator for MicroMDM. It enables profiles to be managed with MicroMDM in a stateful manner, via a RESTful API. It also allows for installation of packages either just at enrollment or immediately. It uses MicroMDM's webhook functionality to recieve events from MicroMDM and then instructs MicroMDM to perform appropriate action. As such, MDMDirector does not need to be exposed to the public internet.

## Usage

MDMDirector is a compiled binary - it has no external dependencies other than a Postgresql database and optionally a signing certificate for signing profiles. It is configured using flags.

### MicroMDM Setup

You must set the `-command-webhook-url` flag on MicroMDM to be the URL that your MDMDirector instance is served on (with the additon of `/webhook`)

```
-command-webhook-url=https://mdmdirector.company.com/webhook
```

### Flags

* `-cert /path/to/certificate` - Path to the signing certificate or p12 file.
* `-db-username string` - (Required) Username used to connect to the postgres instance.
* `-db-password string` - (Required) Password of the DB user.
* `-db-name string` - (Required) Name of the database to connect to.
* `-db-host string` - (Required) Hostname or IP of the postgres instance
* `-debug` - Enable debug mode. Does things like shorten intervals for scheduled tasks. Only to be used during development.
* `-escrowurl` - HTTP endpoint to escrow erase and unlock PINs to ([Crypt](https://github.com/grahamgilbert/crypt-server) and other compatible servers).
* `-key-password string` - Password to decrypt the signing key or p12 file.
* `-loglevel string` - Log level. One of debug, info, warn, error (default "warn")
* `-micromdmapikey string` - (Required) MicroMDM Server API Key
* `-micromdmurl string` - (Required) MicroMDM Server URL
* `-password string` - (Required) Password used for basic authentication
* `-port string` - Port number to run mdmdirector on. (default "8000")
* `-signing-private-key string` - Path to the signing private key. Don't use with p12 file.
* `-push-new-build` - Re-push profiles if the device's build number changes. (default true)
* `-sign` - Sign profiles prior to sending to MicroMDM. Requires `-cert` to be passed.

## Todo

### Documentation

* Posting / removing profiles and apps
* Example for systemd

### App

* App state inspection binary (perhaps a separate service to MDMDirector due to requiring exposure to the public internet)
* FileVault Key escrow to Crypt (and compatible servers)
* Re-push enrollment profile when SCEP certificate is coming up to expiry

## Contributing

* File issues
* Open Pull Requests
