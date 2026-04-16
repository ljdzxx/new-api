#!/bin/sh

stty echo
set -x

ps -ef | grep new-api | grep -v grep | awk 'NR==1{print $2}' | xargs -r kill

git pull

sleep 3
#cd web
#bun run build
#cd ..
rm -rf ./new-api
go build -tags noweb -o new-api
echo 'build finished.'
