#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
"$ROOT/scripts/build-macos-app.sh" "${1:-0.1.0}"
open "$ROOT/dist/Cooler Compiler Lab.app"
