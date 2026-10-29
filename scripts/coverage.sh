#!/usr/bin/env bash
# coverage.sh — test coverage per system.
#
# Runs the unit tests WITHOUT -short (the nvim-based tests count; nvim must be
# on PATH), merges one profile for the repo's shipped packages and prints a
# table per system plus the biggest uncovered functions of each.
#
#   scripts/coverage.sh                 # unit tests
#   scripts/coverage.sh -e2e            # plus the e2e suite (cover build)
#   scripts/coverage.sh -min 95         # exit 1 if any system is below 95%
#   scripts/coverage.sh -profile f.out  # only report an existing profile
#
# The systems are listed in scripts/coverage-systems.txt. See `-h` for flags.
# It runs at the lowest CPU priority with GOMAXPROCS=4 unless you set it.
set -eu
cd "$(dirname "$0")/.."
exec go run ./cmd/covreport "$@"
