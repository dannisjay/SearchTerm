#!/bin/bash
set -e
cd /src
gofmt -w ./cmd ./internal
go vet ./...
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /src/searchterm ./cmd/server
ls -lh /src/searchterm
