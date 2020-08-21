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
