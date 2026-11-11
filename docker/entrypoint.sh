#!/bin/sh
# Папка данных должна принадлежать пользователю, от которого работает программа (PUID:PGID).
# Сама программа запускается не от root.
set -e
mkdir -p /data
chown -R "${PUID}:${PGID}" /data
chmod 700 /data
exec su-exec "${PUID}:${PGID}" /usr/local/bin/rfbs
