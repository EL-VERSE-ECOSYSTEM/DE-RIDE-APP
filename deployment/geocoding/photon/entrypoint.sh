#!/bin/sh
set -eu

cd /photon
if [ ! -f /photon/photon_data/nominatim-import-complete ]; then
  : "${NOMINATIM_PASSWORD:?NOMINATIM_PASSWORD is required}"
  until nc -z nominatim 5432; do
    echo 'Waiting for Nominatim PostgreSQL...'
    sleep 5
  done
  java -jar /photon/photon.jar import \
    -host nominatim -port 5432 -database nominatim -user www-data -password "$NOMINATIM_PASSWORD" \
    -country-codes NG -languages en
  touch /photon/photon_data/nominatim-import-complete
fi
exec java -jar /photon/photon.jar serve -listen-ip 0.0.0.0
