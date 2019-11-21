#!/bin/sh

# The following is a bash wrapper to use ENV vs Flags.

# Set Environmental VARS.
# Postgress DB Connection requirements
DB_PORT="${DB_PORT:-5432}"
DB_NAME="${DB_NAME:-postgress}"
DB_PASSWORD="${DB_PASSWORD:-password}"
DB_SSL="${DB_SSL:-disable}"
DB_HOST="${DB_HOST:-127.0.0.1}"
DB_USER="${DB_USER:-postgres}"

# MicroMDM Requred info
MICRO_API_KEY="${MICRO_API_KEY}"
MICRO_URL="${MICRO_URL}"

#DirectorSetup
DIRECTOR_PASSWORD="${DIRECTOR_PASSWORD}"
DIRECTOR_PORT="${DIRECTOR_PORT:-8000}"

# Codesigning
SIGNING_CERT="${SIGNING_CERT}"
SIGNING_KEY="${SIGNING_KEY}"
SIGNING_PASSWORD="${SIGNING_PASSWORD}"
SIGN="${SIGN}"

DEBUG="${DEBUG}"

runMDMDDirector="/usr/bin/mdmdirector \
  -dbconnection host='${DB_HOST}' port='${DB_PORT}' user='${DB_USER}' dbname='${DB_NAME}' password='${DB_PASSWORD}' sslmode='${DB_SSL}' \
  -micromdmapikey '${MICRO_API_KEY}' \
  -micromdmurl '${MICRO_URL}' \
  -password '${DIRECTOR_PASSWORD}' \
  -port '${DIRECTOR_PORT}'"

# Process Codesigning
if [[ ${SIGN} ]]; then
  if [[ ${SIGNING_CERT} ]] && [[ ${SIGNING_KEY} ]]; then
    runMicroMDM="${runMicroMDM} \
      -cert '${SIGNING_CERT}' \
      -private-key '${SIGNING_KEY}' \
      -sign"
  elif [[ ${SIGNING_CERT} ]] && [[ ${SIGNING_PASSWORD} ]]; then
    runMicroMDM="${runMicroMDM} \
      -cert '${SIGNING_CERT}' \
      -key-password  '${SIGNING_PASSWORD}' \
      -sign"
  fi
fi

# process debugging
if [[ ${DEBUG} ]]; then
  runMDMDDirector="${runMDMDDirector} \
    -debug"
fi
echo $runMDMDDirector
eval "${runMDMDDirector}"
