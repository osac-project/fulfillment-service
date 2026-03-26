#!/bin/bash

# Directory containing the database connection parameters:
param_dir="/opt/keycloak/db"

# Regular expression to match the URL and extract the components:
url_regex="^postgres://(([^:@]+)(:([^@]+))?@)?([^:/]+)(:([0-9]+))?/(.*)$"

# Default values for database connection parameters:
db_host="localhost"
db_port="5432"
db_name="postgres"
declare -A db_params

# Load actual values from the files:
for param_path in "${param_dir}"/*; do
  param_name=$(basename "${param_path}")
  param_value=$(tr -d '\0' < "${param_path}")
  case "${param_name}" in
    "url")
      if [[ ! "${param_value}" =~ ${url_regex} ]]; then
        echo "URL from file '${param_path}' is invalid, it doesn't match regular expression '${url_regex}'"
        exit 1
      fi
      [[ -n "${BASH_REMATCH[2]}" ]] && db_user="${BASH_REMATCH[2]}"
      [[ -n "${BASH_REMATCH[4]}" ]] && db_password="${BASH_REMATCH[4]}"
      [[ -n "${BASH_REMATCH[5]}" ]] && db_host="${BASH_REMATCH[5]}"
      [[ -n "${BASH_REMATCH[7]}" ]] && db_port="${BASH_REMATCH[7]}"
      [[ -n "${BASH_REMATCH[8]}" ]] && db_name="${BASH_REMATCH[8]}"
      ;;
    "user")
      db_user="${param_value}"
      ;;
    "password")
      db_password="${param_value}"
      ;;
    "host")
      db_host="${param_value}"
      ;;
    "port")
      db_port="${param_value}"
      ;;
    "dbname")
      db_name="${param_value}"
      ;;
    "sslcert" | "sslkey" | "sslrootcert")
      db_params["${param_name}"]="${param_path}"
      ;;
    *)
      db_params["${param_name}"]="${param_value}"
      ;;
  esac
done

# Rebuild the URL without the user and password, as they are stored in separate environment variables:
export KC_DB_URL="jdbc:postgresql://${db_host}:${db_port}/${db_name}"
index="0"
for param_name in "${!db_params[@]}"; do
  if [[ "${index}" == "0" ]]; then
    KC_DB_URL+="?"
  else
    KC_DB_URL+="&"
  fi
  param_value="${db_params[${param_name}]}"
  KC_DB_URL+="${param_name}=${param_value}"
  ((index++))
done

# Set the user and password environment variables:
export KC_DB_USERNAME="${db_user}"
export KC_DB_PASSWORD="${db_password}"

# Start Keycloak:
exec /opt/keycloak/bin/kc.sh start --import-realm
