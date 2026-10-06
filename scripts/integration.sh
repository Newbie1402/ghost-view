#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go test ./internal/provider/... ./internal/handler/... ./internal/downloader/...
bash scripts/acceptance.sh
