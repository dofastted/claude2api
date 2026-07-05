#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$ROOT_DIR/backend"
go test ./internal/service -run '^TestAutoresearchProfileContract' -count=1 >/dev/null

cd "$ROOT_DIR"
profile_contract_gaps="$({
python3 - <<'PY'
from pathlib import Path

root = Path.cwd()
checks = 0

codex_transform = (root / "backend/internal/service/openai_codex_transform.go").read_text(encoding="utf-8")
# User-visible prompt/instructions rewrites are tracked as gaps for this research target.
checks += codex_transform.count("extractSystemMessagesFromInput(reqBody)")
checks += codex_transform.count("applyInstructions(reqBody")

# Current project has no CPA-style codex.identity-confuse remapping entrypoint yet.
service_sources = "\n".join(p.read_text(encoding="utf-8") for p in (root / "backend/internal/service").glob("*.go"))
if "IdentityConfuse" not in service_sources and "identity-confuse" not in service_sources:
    checks += 1

print(checks)
PY
} )"

printf 'METRIC profile_contract_gaps=%s\n' "$profile_contract_gaps"
printf 'METRIC profile_contract_tests=2\n'
printf 'METRIC cpa_reference_files=2\n'
