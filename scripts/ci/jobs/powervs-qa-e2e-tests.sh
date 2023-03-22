#!/usr/bin/env bash

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")"/../../.. && pwd)"
source "$ROOT/scripts/ci/lib.sh"

set -euo pipefail

run_powervs_tests() {
    info "PowerVS QA e2e tests"

    export DEPLOY_STACKROX_VIA_OPERATOR="true"
    
    python3 -u rosa_qa_e2e_tests.py
}

run_powervs_tests "$*"
