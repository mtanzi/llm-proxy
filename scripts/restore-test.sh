#!/bin/sh
# Restore test (ADR 0037, runbook "Backups"): record a call, dump Postgres with
# pg_dump, restore the dump into a fresh Postgres, start serve against it, and
# open the recorded call through the admin API and the UI. These are the runbook
# commands, run against a throwaway compose stack.
# Usage: restore-test.sh IMAGE
set -eu
# shellcheck source=scripts/compose-test-lib.sh
. "$(dirname "$0")/compose-test-lib.sh"

ct_setup "${1:?usage: restore-test.sh IMAGE}"

# Stack A: the source. One call through the proxy, then a dump.
a=llm-proxy-rt-a-$$
ct_start "$a"
[ "$(ct_send)" = 200 ] || {
	echo "FAIL request through the proxy" >&2
	exit 1
}
ct_wait_call "$a"
want=$call_id
echo "ok  recorded call $want"
ct_compose "$a" exec -T postgres pg_dump -U llmproxy -Fc llmproxy >"$tmp/llmproxy.dump"
[ -s "$tmp/llmproxy.dump" ] || {
	echo "FAIL pg_dump wrote nothing" >&2
	exit 1
}
echo "ok  pg_dump wrote $(wc -c <"$tmp/llmproxy.dump" | tr -d ' ') bytes"
ct_compose "$a" down -v --timeout 20 >/dev/null 2>&1

# Stack B: a fresh Postgres with a new volume. Restore, then start serve.
b=llm-proxy-rt-b-$$
projects="$projects $b"
ct_compose "$b" up -d --wait --wait-timeout 120 postgres fake-upstream
ct_compose "$b" exec -T postgres pg_restore -U llmproxy -d llmproxy --no-owner <"$tmp/llmproxy.dump"
echo "ok  pg_restore into a fresh Postgres"
ct_start "$b"

# The recent call opens by id, and it is the one recorded before the dump.
detail=$(ct_admin "$b" -f "http://127.0.0.1:9090/admin/calls/$want")
case $detail in
*"\"id\":\"$want\""*"\"key\":\"my-laptop\""*) echo "ok  call $want opens after the restore" ;;
*)
	echo "FAIL call $want did not open after the restore: $detail" >&2
	exit 1
	;;
esac
ct_check_ui "$b"
echo "restore test passed"
