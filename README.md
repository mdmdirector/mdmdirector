# MDMDirector

MDMDirector is an opinionated orchestrator for [MicroMDM](https://github.com/micromdm/micromdm) or [NanoMDM](https://github.com/micromdm/nanomdm). It enables profiles to be managed in a stateful manner, via a RESTful API. It also allows for installation of packages either just at enrollment or immediately. It receives webhook events from your MDM server and then instructs it to perform appropriate actions. As such, MDMDirector does not need to be exposed to the public internet.

When using NanoMDM, MDMDirector can also integrate with [KMFDDM](https://github.com/jessepeterson/kmfddm) to manage profiles and packages via Apple's Declarative Device Management (DDM) protocol.

## Usage

MDMDirector is a compiled binary and is configured using flags.

Requirements:

* Redis for the scheduled checkin queue
* PostgreSQL database for storing device information
* (Recommended) Signing certificate for signing profiles
* (STRONGLY recommended) Load balancer/proxy to serve and terminate TLS for MDMDirector


### MicroMDM Setup

You must set the `-command-webhook-url` flag on MicroMDM to the URL of your MDMDirector instance (with the addition of `/webhook`).

```
-command-webhook-url=https://mdmdirector.company.com/webhook
```

### NanoMDM Setup

For NanoMDM, configure the webhook URL similarly and set `-mdm-server-type=nanomdm` along with the NanoMDM-specific flags.

If you want to use Declarative Device Management (DDM) for profile and/or package management, you'll also need to run [KMFDDM](https://github.com/jessepeterson/kmfddm) and configure the DDM-related flags.

### Flags

#### Core Settings

- `-port string` - Port number to run MDMDirector on. (default "8000")
- `-password string` - **(Required)** Password used for basic authentication. Env: `DIRECTOR_PASSWORD`
- `-debug` - Enable debug mode. Shortens intervals for scheduled tasks. Only for development. Env: `DEBUG`
- `-loglevel string` - Log level. One of debug, info, warn, error. (default "warn") Env: `LOG_LEVEL`
- `-log-format string` - Log format. Either `logfmt` (default) or `json`. Env: `LOG_FORMAT`
- `-prometheus` - Enable Prometheus metrics endpoint at `/metrics`. (default false) Env: `PROMETHEUS`

#### MDM Server Configuration

- `-mdm-server-type string` - MDM server type: `micromdm` or `nanomdm`. (default "micromdm") Env: `MDM_SERVER_TYPE`

##### MicroMDM (default)

- `-micromdmurl string` - **(Required if using MicroMDM)** MicroMDM Server URL. Env: `MICRO_URL`
- `-micromdmapikey string` - **(Required if using MicroMDM)** MicroMDM Server API Key. Env: `MICRO_API_KEY`

##### NanoMDM

- `-nanomdm-url string` - **(Required if mdm-server-type=nanomdm)** NanoMDM server URL. Env: `NANOMDM_URL`
- `-nanomdm-api-key string` - **(Required if mdm-server-type=nanomdm)** NanoMDM server API key. Env: `NANOMDM_API_KEY`

#### DDM / KMFDDM Configuration

These flags enable Declarative Device Management via KMFDDM. DDM requires `mdm-server-type=nanomdm`.

- `-use-ddm` - Enable DDM profile management via KMFDDM instead of InstallProfile commands. (default false) Env: `USE_DDM`
- `-use-ddm-packages` - Enable DDM package management via KMFDDM instead of InstallApplication commands. (default false) Env: `USE_DDM_PACKAGES`
- `-kmfddm-url string` - **(Required if DDM enabled)** KMFDDM server base URL. Env: `KMFDDM_URL`
- `-kmfddm-api-key string` - **(Required if DDM enabled)** KMFDDM API key for basic auth. Env: `KMFDDM_API_KEY`
- `-ddm-declaration-prefix string` - **(Required if DDM enabled)** Reverse-DNS prefix for DDM declaration identifiers (e.g. `com.example.mdm`). Env: `DDM_DECLARATION_PREFIX`

#### Database Configuration

- `-db-host string` - **(Required)** Hostname or IP of the PostgreSQL instance. Env: `DB_HOST`
- `-db-port string` - Port of the PostgreSQL instance. (default "5432") Env: `DB_PORT`
- `-db-name string` - **(Required)** Name of the database to connect to. Env: `DB_NAME`
- `-db-username string` - **(Required)** Username used to connect to the PostgreSQL instance. Env: `DB_USERNAME`
- `-db-password string` - **(Required)** Password of the DB user. Env: `DB_PASSWORD`
- `-db-sslmode string` - SSL Mode to use to connect to PostgreSQL. (default "disable")
- `-db-max-idle-connections int` - Maximum number of connections in the idle connection pool. (default -1, uses Go sql package default)
- `-db-max-connections int` - Maximum number of database connections. (default 100)

#### Redis Configuration

- `-redis-host string` - Hostname of your Redis instance. (default "localhost") Env: `REDIS_HOST`
- `-redis-port string` - Port of your Redis instance. (default "6379") Env: `REDIS_PORT`
- `-redis-password string` - Password for your Redis instance. (default "") Env: `REDIS_PASSWORD`
- `-redis-tls` - Enable TLS for the Redis connection. Required when connecting to ElastiCache with transit encryption enabled. (default false) Env: `REDIS_TLS`

#### Profile Signing

- `-sign` - Sign profiles prior to sending to MDM server. Requires `-cert` to be passed. Env: `SIGN`
- `-cert string` - Path to the signing certificate or p12 file. Env: `SIGNING_CERT`
- `-signing-private-key string` - Path to the signing private key. Don't use with p12 file. Env: `SIGNING_KEY`
- `-key-password string` - Password to decrypt the signing key or p12 file. Env: `SIGNING_PASSWORD`

#### Enrollment Configuration

- `-enrollment-profile string` - Path to local enrollment profile for re-enrollment. Env: `ENROLLMENT_PROFILE`
- `-enrollment-profile-signed` - Is the enrollment profile already signed. (default false) Env: `ENROLMENT_PROFILE_SIGNED`
- `-clear-device-on-enroll` - Deletes device profiles and install applications when a device enrolls. (default false) Env: `CLEAR_DEVICE_ON_ENROLL`

##### Enrollment Webhook (Remote Profile Fetching)

- `-enable-reenroll-via-webhook` - Enable fetching the enrollment profile from a remote webhook for re-enrollment. (default false) Env: `ENABLE_REENROLL_VIA_WEBHOOK`
- `-enroll-webhook-url string` - **(Required if webhook enabled)** URL of the enrollment profile webhook endpoint. Env: `ENROLL_WEBHOOK_URL`
- `-enroll-webhook-token string` - **(Required if webhook enabled)** Bearer token for the enrollment profile webhook. Env: `ENROLL_WEBHOOK_TOKEN`

#### Certificate Renewal

- `-scep-cert-issuer string` - Issuer of your SCEP certificate. (default "OU=MICROMDM SCEP CA,O=MicroMDM,C=US") Env: `SCEP_CERT_ISSUER`
- `-scep-cert-min-validity int` - Days remaining on SCEP certificate before re-enrollment is triggered. (default 180) Env: `SCEP_CERT_MIN_VALIDITY`
- `-acme-cert-issuer string` - Issuer of your ACME certificate. When set, ACME cert expiry will also be checked. Env: `ACME_CERT_ISSUER`
- `-acme-cert-min-validity int` - Days remaining on ACME certificate before re-enrollment is triggered. (default 180) Env: `ACME_CERT_MIN_VALIDITY`

#### Scheduling and Intervals

- `-push-new-build` - Re-push profiles if the device's build number changes. (default true) Env: `PUSH_NEW_BUILD`
- `-once-in int` - Minutes to wait before queuing additional commands for a device with pending commands. (default 60, overridden to 2 if --debug) Env: `ONCE_IN`
- `-info-request-interval int` - Minutes between issuing DeviceInfo, ProfileList, SecurityInfo commands. (default 360) Env: `INFO_REQUEST_INTERVAL`

#### PIN Escrow

- `-escrowurl string` - HTTP(S) endpoint to escrow erase and unlock PINs to ([Crypt](https://github.com/grahamgilbert/crypt-server) compatible). Env: `ESCROW_URL`


## Todo

### Documentation

- Posting / removing profiles and apps

### App

- App state inspection binary (perhaps a separate service to MDMDirector due to requiring exposure to the public internet)

## Contributing

- File issues
- Open Pull Requests
