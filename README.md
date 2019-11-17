# MDMDirector

MDMDirector is an opinionated orchestrator for MicroMDM. It enables profiles to be managed with MicroMDM in a stateful manner, via a RESTful API. It also allows for installation of packages either just at enrollment or immediately. It uses MicroMDM's webhook functionality to recieve events from MicroMDM and then instructs MicroMDM to perform appropriate action. As such, MDMDirector does not need to be exposed to the public internet.

## Usage

MDMDirector is a compiled binary - it has no external dependencies other than a Postgresql database and optionally a signing certificate for signing profiles. It is configured using flags.

### Flags

- `-cert /path/to/certificate` - Path to the signing certificate or p12 file.
- `-dbconnection yourconnectionstring` - (Required) Database connection string. Example: `host=127.0.0.1 port=5432 user=postgres dbname=postgres password=password sslmode=disable`
- `-debug` - Enable debug mode. Does things like shorten intervals for scheduled tasks. Only to be used during development.
- `-key-password string` - Password to decrypt the signing key or p12 file.
- `-loglevel string` - Log level. One of debug, info, warn, error (default "warn")
- `-micromdmapikey string` - (Required) MicroMDM Server API Key
- `-micromdmurl string` - (Required) MicroMDM Server URL
- `-password string` - (Required) Password used for basic authentication
- `-port string` - Port number to run mdmdirector on. (default "8000")
- `-private-key string` - Path to the signing private key. Don't use with p12 file.
- `-push-new-build` - Re-push profiles if the device's build number changes. (default true)
- `-sign` - Sign profiles prior to sending to MicroMDM. Requires `-cert` to be passed.

## Todo

### Documentation

- Posting / removing profiles and apps
- [Example for systemd](docs/Managing-mdmdirector-with-systemd.md)

### App

- Support for Lock/Wipe
- App state inspection binary (perhaps a separate service to MDMDirector due to requiring exposure to the public internet)
- FileVault Key escrow to Crypt (and compatible servers)
- Re-push enrollment profile when SCEP certificate is coming up to expiry

## Contributing

- File issues
- Open Pull Requests
