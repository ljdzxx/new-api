#!/bin/sh

stty echo
set -x

#ps -ef | grep new-api-13001 | grep -v grep | awk 'NR==1{print $2}' | xargs -r kill
lsof -ti:13001 | head -n1 | xargs -r kill

git pull

sleep 3
#cd web
#bun run build
#cd ..
rm -rf ./new-api-13001
go build -tags noweb -o new-api-13001
echo 'build new-api-13001 finished.'
