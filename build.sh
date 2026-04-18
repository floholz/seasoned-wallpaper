#!/usr/bin/env bash
set -euo pipefail

version="${1:-dev}"
go build -o seasoned -ldflags "-X main.version=${version}" ./cmd/seasoned
