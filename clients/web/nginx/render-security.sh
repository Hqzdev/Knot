#!/bin/sh
set -eu

envsubst '${KNOT_OBJECTS_ORIGIN}' \
  < /etc/nginx/templates-extra/knot-security.conf.template \
  > /tmp/knot-security.conf
