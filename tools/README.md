# Samples of Common API requests to get you started using MDMDirector.

Credit to [@groob](https://github.com/groob) for providing the intial guidance in https://github.com/micromdm/micromdm/pull/392

## Requirements

- [jq](https://stedolan.github.io/jq/)
  `brew install jq`

## Setup

Create an `env` file and define environment variables you will need to talk to the server.
This env file will be sourced by the scripts.

Contents of `env` file:

```
# the value of the -api-key flag that MDMDirector was started with.
export API_TOKEN=supersecret
export SERVER_URL=https://mdmdirector.acme.co
```

In your shell, set the environment variable `MDMDIRECTOR_ENV_PATH` to point to your env file.
Do this every time you open a new shell to work with the scripts in this folder.

```
export MDMDIRECTOR_ENV_PATH="$(pwd)/env"
```

For security of the credentials ensure to appropriately lock down the file permissions

```
chmod 600 filename
```

## Usage examples

### Install a shared application on all devices

```
./tools/post_shared_install_application https://example.com/app.plist
```

### Delete a shared application from all devices

Removes the application from MDMDirector's DB and, if DDM package management is enabled,
removes the corresponding declarations from KMFDDM and notifies all devices to sync.

```
./tools/delete_shared_install_application https://example.com/app.plist
```

## Declarative Device Management (DDM)

These scripts drive the per-device DDM API. All take a device UDID.

There are two distinct ways to put a device on DDM:

- **Activate** (`ddm_activate`) sends only the `DeclarativeManagement` command so the device
  turns on its declarative engine and can use DDM as needed. It **converts nothing** — no
  profiles or apps become declarations — and writes no opt-in state.
- **Enable** (`ddm_enable`) opts the device in and **converts** its existing profiles and
  apps into declarations via KMFDDM, so subsequent pushes use DDM.

### Activate DDM on a device (no conversion)

```
./tools/ddm_activate $udid
```

Whole-fleet activation is not an API call — start the server with `--activate-ddm-fleet`
(env `ACTIVATE_DDM_FLEET`) to send the bare command to every device at startup.

### Query a device's DDM enabled status

Reports whether the device actually turned on the declarative engine, confirmed from
KMFDDM status reports: `{device_udid, ddm_enabled, status_count, last_status_at}`.

```
./tools/ddm_status $udid
```

### Enable DDM on a device (convert profiles and apps)

```
./tools/ddm_enable $udid
```

### Disable DDM on a device

Tears down the device's DDM profile declarations and re-pushes profiles via `InstallProfile`
commands. Applications already installed via DDM are left in place.

```
./tools/ddm_disable $udid
```
