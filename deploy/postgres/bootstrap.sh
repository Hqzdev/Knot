#!/bin/sh
set -eu

for database in knot_identity knot_delivery knot_push knot_attachments; do
  existing="$(psql -h postgres -U knot -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$database'")"
  if [ "$existing" != "1" ]; then
    psql -h postgres -U knot -d postgres -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"$database\""
  fi
done
