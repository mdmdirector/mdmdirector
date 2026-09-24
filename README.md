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

- `-port string` - Port number to run MDMDirector on. (default "8000") Env: `DIRECTOR_PORT`
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
- `-nanomdm-profile-url string` - Public NanoMDM URL that devices can reach. Used to build the DDM profile-download URLs that devices fetch directly through NanoMDM's authproxy. Defaults to `-nanomdm-url`. Set this when the server-to-server URL is an internal address. Env: `NANOMDM_PROFILE_URL`
- `-dual-write-micromdm` - Mirror every checkin webhook to the legacy MicroMDM (`PUT <micromdmurl>/mdm/migration-checkin`) as a rollback safety net during a MicroMDM to NanoMDM cutover. Requires `-mdm-server-type=nanomdm` plus `-micromdmurl` / `-micromdmapikey`. Failures are logged only. (default false) Env: `DUAL_WRITE_MICROMDM`

#### DDM / KMFDDM Configuration

These flags enable Declarative Device Management via KMFDDM. DDM requires `mdm-server-type=nanomdm`.

- `-use-ddm` - Enable DDM profile management via KMFDDM instead of InstallProfile commands. (default false) Env: `USE_DDM`
- `-use-ddm-packages` - Enable DDM package management via KMFDDM instead of InstallApplication commands. (default false) Env: `USE_DDM_PACKAGES`
- `-kmfddm-url string` - **(Required if DDM enabled)** KMFDDM server base URL. Env: `KMFDDM_URL`
- `-kmfddm-api-key string` - **(Required if DDM enabled)** KMFDDM API key for basic auth. Env: `KMFDDM_API_KEY`
- `-ddm-declaration-prefix string` - **(Required if DDM enabled)** Reverse-DNS prefix for DDM declaration identifiers (e.g. `com.example.mdm`). Env: `DDM_DECLARATION_PREFIX`
- `-activate-ddm-fleet` - At startup, send a bare `DeclarativeManagement` command to every device so its declarative engine is on. Converts nothing and writes no opt-in state. See [DDM](#declarative-device-management-ddm). (default false) Env: `ACTIVATE_DDM_FLEET`

#### Database Configuration

- `-db-host string` - **(Required)** Hostname or IP of the PostgreSQL instance. Env: `DB_HOST`
- `-db-port string` - Port of the PostgreSQL instance. (default "5432") Env: `DB_PORT`
- `-db-name string` - **(Required)** Name of the database to connect to. Env: `DB_NAME`
- `-db-username string` - **(Required)** Username used to connect to the PostgreSQL instance. Env: `DB_USERNAME`
- `-db-password string` - **(Required)** Password of the DB user. Env: `DB_PASSWORD`
- `-db-sslmode string` - SSL Mode to use to connect to PostgreSQL. (default "disable")
- `-db-max-idle-connections int` - Maximum number of connections in the idle connection pool. (default -1, uses Go sql package default)
- `-db-max-connections int` - Maximum number of database connections. (default 100)
- `-db-conn-max-idle-time int` - Seconds a pooled connection may sit idle before it is closed. Keep this below any idle timeout enforced between MDMDirector and PostgreSQL (service mesh, NLB, RDS proxy) so the pool recycles connections before the network does. 0 disables. (default 240) Env: `DB_CONN_MAX_IDLE_TIME`
- `-db-conn-max-lifetime int` - Seconds a pooled connection may be reused before it is closed. 0 means forever. (default 1800) Env: `DB_CONN_MAX_LIFETIME`

#### Redis Configuration

- `-redis-host string` - Hostname of your Redis instance. (default "localhost") Env: `REDIS_HOST`
- `-redis-port string` - Port of your Redis instance. (default "6379") Env: `REDIS_PORT`
- `-redis-password string` - Password for your Redis instance. (default "") Env: `REDIS_PASSWORD`
- `-redis-tls` - Enable TLS for the Redis connection. (default false) Env: `REDIS_TLS`

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

  Re-enrollment (both the certificate-expiry check and the enrollment-profile signer check) is
  only ever attempted for devices positively identified as Apple Silicon from their reported
  `Model`. Intel Macs cannot complete the ACME enrollment profile mdmenroll returns and keep their
  existing (SCEP) identity untouched; a device whose model is not yet known is treated the same
  way until its next `DeviceInformation`. Suppressed attempts are counted in
  `mdmdirector_reenroll_skipped_total{trigger,arch}`.

#### Scheduling and Intervals

- `-push-new-build` - Re-push profiles if the device's build number changes. (default true) Env: `PUSH_NEW_BUILD`
- `-once-in int` - Minutes to wait before queuing additional commands for a device with pending commands. Also the minimum gap between scheduled pushes to one device (`next_push`). (default 60, overridden to 2 if --debug) Env: `ONCE_IN`
- `-control-plane-interval int` - Minutes between fleet-wide control-plane scans (scheduled pushes plus DB cleanup). The scan is single-flight across replicas, see [Control plane](#control-plane-and-scheduling). (default 120) Env: `CONTROL_PLANE_INTERVAL`
- `-info-request-interval int` - Minutes between issuing DeviceInfo, ProfileList, SecurityInfo commands. (default 360) Env: `INFO_REQUEST_INTERVAL`

#### PIN Escrow

- `-escrowurl string` - HTTP(S) endpoint to escrow erase and unlock PINs to ([Crypt](https://github.com/grahamgilbert/crypt-server) compatible). Env: `ESCROW_URL`


## HTTP API

All routes except `/webhook`, `/health`, `/metrics` and `/profiledownload/...` require basic auth with user `mdmdirector` and the `-password` value. Ready-made `curl` wrappers for the common calls live in [`tools/`](tools/README.md).

| Method | Path | Purpose | Tool |
|---|---|---|---|
| POST | `/webhook` | MDM server webhook (checkins, command results). Point MicroMDM/NanoMDM here. | |
| GET | `/health` | Liveness/readiness. Pings the DB pool (5s timeout). `200 {"status":"UP"}` or `503 {"status":"DOWN"}`. | |
| GET | `/metrics` | Prometheus metrics when `-prometheus` is set. | |
| POST | `/profile` | Install profiles on `udids` (`["*"]` = shared, all devices). Body: `{udids, profiles[] (base64 mobileconfig), push_now, metadata}`. | `post_profile`, `post_shared_profile` |
| DELETE | `/profile` | Remove profiles by `payload_identifier` from `udids` or `["*"]`. | `delete_profile`, `delete_shared_profile` |
| GET | `/profile` | List shared profiles. | |
| GET | `/profile/{udid}` | List a device's profiles and install state. | |
| GET | `/profiledownload/{udid}/{profileIdentifier}` | Unauthenticated per-device profile download used by DDM declarations. Requires the `X-Enrollment-ID` header to match `{udid}`; NanoMDM's authproxy injects it. | |
| POST | `/installapplication` | Install apps from `manifest_urls[]` on `udids` or `["*"]`. `bootstrap_only` installs only at enrollment. | `post_install_application`, `post_shared_install_application` |
| DELETE | `/installapplication` | Remove a shared app (and its DDM declarations if DDM packages are on). | `delete_shared_install_application` |
| GET | `/installapplication` | List shared apps. | |
| GET | `/device` | List all devices. | |
| GET | `/device/{udid}` | One device by UDID. | |
| GET | `/device/serial/{serial}` | One device by serial. | |
| GET | `/device/push/{udid}` | Push the device now (queue a checkin). | |
| GET | `/device/{udid}/commands` | Inspect the device's pending queue on the MDM server. | `inspect_device_queue` |
| POST | `/device/command/{command}` | Device commands: `device_lock` (`value`, `pin`), `erase_device` (`value`), `clear_queue`. `value:false` on `device_lock` cancels a pending lock. | `device_lock`, `device_unlock`, `erase_device`, `clear_device_queue` |
| DELETE | `/device/command/erase_device` | Cancel a pending erase. | `unerase_device` |
| GET | `/command` | All commands MDMDirector has issued. | |
| GET | `/command/pending` | Commands still pending on devices. | |
| GET | `/command/pending/delete` | Delete pending command records. | |
| GET | `/command/error` | Commands that returned an error. | |
| POST | `/device/{udid}/ddm/activate` | Send a bare `DeclarativeManagement` command (no conversion). | `ddm_activate` |
| POST | `/device/{udid}/ddm` | Opt the device in to DDM and convert its profiles/apps to declarations. | `ddm_enable` |
| DELETE | `/device/{udid}/ddm` | Opt out: remove profile declarations and re-push via `InstallProfile`. | `ddm_disable` |
| GET | `/device/{udid}/ddm/status` | Device-confirmed DDM state from KMFDDM status reports. | `ddm_status` |

## How it works

### Control plane and scheduling

MDMDirector is safe to run with several replicas behind one webhook URL. Webhook handling is stateless per event. The periodic work is single-flight:

- Every `-control-plane-interval` minutes each replica tries to take a Redis lock at `mdmdirector:control-plane` (TTL 30s, refreshed every 10s while held). Only the holder runs the scan; the others skip until the next tick. If the holder dies the lock expires and another replica picks up on its next tick.
- The scan fetches the enrollment list from the MDM server, queues a push for every device whose `next_push` is due (`pushAll`), then runs cleanup: orphaned certificate and profile-list rows, unlock PINs older than 30 minutes, and stale `unlock_pin` values on devices that are no longer locked or erased.
- Pushes go through a Redis-backed `taskq` queue. `PushDevice` sets `next_push = now + ONCE_IN`, and a device is skipped while `next_push` is in the future, so `-once-in` bounds the per-device cadence.
- Shutdown is graceful: `SIGINT`/`SIGTERM` cancels the root context, the queue consumer drains in-flight work, and the HTTP server gets 15s to finish.

### Initial tasks lease

`RunInitialTasks` (the first `DeviceInformation`, `ProfileList`, `SecurityInfo`, profile and app pushes for a new enrollment) is guarded by a lease in the `devices` row: an atomic `UPDATE` sets `run_initial_tasks_starttime` only when it is NULL or older than 5 minutes. The winner runs the tasks; other replicas see zero rows affected and skip. Command acknowledgements that arrive while a run is in flight do not touch the lease at all, so `mdmdirector_initial_tasks_total{result="lease_contention"}` only counts genuine same-instant races. A holder that dies mid-run is recovered after the 5 minute TTL.

### Re-enrollment and certificate renewal

Two triggers can make MDMDirector reinstall the enrollment profile on a device:

1. **Certificate expiry.** The device's `CertificateList` is scanned for its ACME identity (issuer `-acme-cert-issuer`) and legacy SCEP identity (issuer `-scep-cert-issuer`). If an ACME cert exists it takes precedence and only `-acme-cert-min-validity` matters. SCEP is checked only when no ACME cert is present.
2. **Signer mismatch.** With `-sign`, the installed `com.apple.mdm` profile's signer is compared with the current signing certificate (subject, expiry, issuer). A mismatch reinstalls the profile so the fleet follows a signing-cert rotation.

Both triggers fire only for devices positively identified as Apple Silicon from their `Model` (`Mac<n>,<n>`, `VirtualMac*`, or a legacy family at or above the first Silicon generation). Intel and not-yet-known models are left alone and counted in `mdmdirector_reenroll_skipped_total{trigger,arch}`. The profile comes from the `-enrollment-profile` file or, with `-enable-reenroll-via-webhook`, from the webhook: MDMDirector builds a CMS-signed `MachineInfo` plist from the device record and sends it as `X-Apple-Aspen-Deviceinfo` with the bearer token, mirroring what a device sends during DEP enrollment.

### Declarative Device Management (DDM)

DDM requires NanoMDM plus KMFDDM. Declarations are named `<prefix>.<udid>.<kind>.<id>` with kinds `legacy_profile`, `legacy_profile_activation`, `package`, `package_activation`. Profile declarations point devices at `/profiledownload/{udid}/{identifier}` on `-nanomdm-profile-url`.

Rollout is per device. With `-use-ddm` / `-use-ddm-packages` off, nothing changes fleet-wide. A device is put on DDM either by **activate** (bare `DeclarativeManagement` command, engine on, nothing converted, also available fleet-wide via `-activate-ddm-fleet`) or **enable** (writes a `ddm_opt_ins` row and converts the device's profiles and apps into KMFDDM declarations, after which pushes to that device go through DDM). **Disable** removes the profile declarations and re-pushes with `InstallProfile`; DDM-installed apps stay. `ddm/status` reports what the device itself has confirmed to KMFDDM. See [`tools/README.md`](tools/README.md) for the operator scripts.

### Database and outbound HTTP

- Every gorm operation is counted in `mdmdirector_db_operations_total{operation,result}`, where `result` separates `stale_connection` (a connection the network dropped while idle) from `postgres_error`, `timeout`, `canceled`, `not_found`. Go `database/sql` pool stats are exported as `go_sql_*`. Tune `-db-conn-max-idle-time` if `stale_connection` is non-zero.
- Calls to NanoMDM use a retrying client (3 retries, 1.5x backoff, 20s max) on network errors and 5xx other than 501. KMFDDM calls use a 20s timeout without retry.

## Metrics

Enable with `-prometheus`. All names are prefixed `mdmdirector_`.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `checkin_requests_total` | counter | `message_type`, `result` | Webhook checkins by type (Authenticate, TokenUpdate, CheckOut, ...). |
| `command_results_total` | counter | `status`, `result` | Command acknowledgements by Apple status. |
| `push_requests_total` | counter | `result` | Push requests to the MDM server. |
| `enqueue_requests_total` | counter | `command_type`, `result` | Commands enqueued on the MDM server. |
| `profile_operations_total` | counter | `scope`, `operation`, `result` | Profile install/remove work, `scope` shared or device. |
| `application_operations_total` | counter | `scope`, `operation`, `result` | App install work. |
| `profile_verification_mismatches_total` | counter | `scope` | Installed profile differs from expected. |
| `profile_download_requests_total` | counter | `scope`, `outcome` | Hits on `/profiledownload`. |
| `profile_api_requests_total` | counter | `method`, `result` | `POST`/`DELETE /profile` API calls. |
| `initial_tasks_total` | counter | `result` | `success`, `error`, `lease_contention`. |
| `reenroll_skipped_total` | counter | `trigger`, `arch` | Re-enrollments suppressed on Intel/unknown models. |
| `pin_escrow_total` | counter | `result` | PIN escrow posts to `-escrowurl`. |
| `ddm_declaration_writes_total` | counter | `declaration_type`, `declaration_subtype`, `operation`, `result` | KMFDDM declaration writes. |
| `ddm_set_membership_changes_total` | counter | same | KMFDDM set membership changes. |
| `ddm_notify_total` | counter | `result` | KMFDDM notify calls. |
| `db_operations_total` | counter | `operation`, `result` | gorm operations, see above. |
| `devices_total` | gauge | | Devices in the DB (polled every minute). |
| `profiles_total` | gauge | `scope`, `installed` | Profiles by scope and install state. |
| `ddm_enabled_devices_total` | gauge | | Rows in `ddm_opt_ins`. |
| `signing_cert_not_after_timestamp_seconds` | gauge | | Expiry of the loaded signing certificate. Alert well before it. |
| `go_sql_*` | mixed | | `database/sql` pool stats. |

## Development

```
mise install          # pins golangci-lint (see mise.toml)
mise run lint         # golangci-lint run
go test ./...
```

Tests use `sqlmock` for the database and `httptest` servers for NanoMDM/KMFDDM, so no services are needed locally.

## Todo

### App

- App state inspection binary (perhaps a separate service to MDMDirector due to requiring exposure to the public internet)

## Contributing

- File issues
- Open Pull Requests
