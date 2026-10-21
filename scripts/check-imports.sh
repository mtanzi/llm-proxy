#!/usr/bin/env bash
# Enforces the dependency rules from docs/architecture/backend.md
# ("Layers and the dependency rule") on the direct imports of every package,
# test files included. Mirrors the depguard rules in .golangci.yml.
set -euo pipefail

mod=$(go list -m)
fail=0

# rule <package-glob> <allowed|denied> <regex>
#   allowed: every non-stdlib-or-module import must match the regex
#   denied:  no import may match the regex
rule() {
	local glob=$1 mode=$2 re=$3
	while read -r pkg imports; do
		[[ $pkg == "$mod/internal/"$glob || $pkg == "$mod/internal/"$glob/* ]] || continue
		for imp in $imports; do
			if [[ $mode == denied ]]; then
				if [[ $imp =~ $re ]]; then
					echo "FAIL: $pkg imports $imp (denied: $re)"
					fail=1
				fi
			else
				# stdlib has no dot in its first path element
				[[ ${imp%%/*} != *.* ]] && continue
				if ! [[ $imp =~ $re ]]; then
					echo "FAIL: $pkg imports $imp (allowed: stdlib or $re)"
					fail=1
				fi
			fi
		done
	done <<<"$all"
}

all=$(go list -e -f '{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}' ./...)

db='^(database/sql|github\.com/jackc/|github\.com/pressly/goose|github\.com/lib/pq)'
store="^$mod/internal/store/"

rule core allowed '^$'
rule family allowed "^($mod/internal/core|github\.com/tidwall/gjson)(/|$)"
rule recorder allowed "^$mod/internal/core(/|$)"
rule keyring allowed "^$mod/internal/core(/|$)"
rule recorder denied "$db|$store"
rule keyring denied "$db|$store"
rule api denied "$db|$store"
rule store/postgres allowed "^($mod/internal/core|github\.com/jackc/|github\.com/pressly/goose)(/|$)"
rule proxy denied "$store"

exit $fail
