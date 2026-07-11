#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")"

if [ ! -f config/config.json ]; then
  cp config/config.example.json config/config.json
  echo "Created config/config.json. Edit it first, then run this script again."
  exit 1
fi

docker compose up -d --build

