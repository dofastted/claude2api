#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/backend"
go test ./internal/service -run TestAutoresearchProfileConflictWorkload -count=1 -v
