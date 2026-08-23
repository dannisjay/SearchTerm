#!/bin/bash
set -e
cd "$(dirname "$0")/.."
mkdir -p dist
docker run --rm -v "$PWD":/src -w /src -e CGO_ENABLED=0 -e GOOS=linux -e GOARCH=amd64 golang:1.24 \
  go build -trimpath -ldflags="-s -w" -o /src/dist/searchterm-linux-amd64 ./cmd/server
docker run --rm -v "$PWD":/src -w /src -e CGO_ENABLED=0 -e GOOS=windows -e GOARCH=amd64 golang:1.24 \
  go build -trimpath -ldflags="-s -w" -o /src/dist/searchterm-windows-amd64.exe ./cmd/server
ls -la dist
