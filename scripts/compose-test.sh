#!/bin/sh
# Test docker-compose.yml (ADR 0037): the file parses, the stack starts and
# becomes healthy, a request through the data plane reaches a fake upstream, the
# call is in the call log, the UI is served, and SIGHUP reloads the config.
# Usage: compose-test.sh IMAGE
set -eu
# shellcheck source=scripts/compose-test-lib.sh
. "$(dirname "$0")/compose-test-lib.sh"

ct_setup "${1:?usage: compose-test.sh IMAGE}"

# The compose file parses, and the shutdown grace period matches the drain.
ct_compose llm-proxy-ct config >"$tmp/compose.rendered"
grep -q 'stop_grace_period: 10m' "$tmp/compose.rendered" || {
	echo "FAIL stop_grace_period is not 10m" >&2
	exit 1
}
echo "ok  docker compose config"

p=llm-proxy-ct-$$
ct_start "$p"

[ "$(ct_send)" = 200 ] || {
	echo "FAIL request through the proxy: $(cat "$tmp/reply.json")" >&2
	exit 1
}
grep -q msg_compose_test "$tmp/reply.json" || {
	echo "FAIL the reply did not come from the fake upstream" >&2
	exit 1
}
echo "ok  request reached the fake upstream"

ct_wait_call "$p"
ct_admin "$p" -f "http://127.0.0.1:9090/admin/calls/$call_id" | grep -q '"key":"my-laptop"' || {
	echo "FAIL the call is not recorded under the key my-laptop" >&2
	exit 1
}
echo "ok  call $call_id is in the call log"

ct_check_ui "$p"

# The data plane rejects a request without a key, and the admin plane is not
# published.
[ "$(curl -s -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:$port/v1/messages")" = 401 ] || {
	echo "FAIL a request without a key was not rejected with 401" >&2
	exit 1
}
if curl -s -o /dev/null --max-time 3 "http://127.0.0.1:9090/healthz"; then
	echo "FAIL the admin plane answers on the host" >&2
	exit 1
fi
echo "ok  401 without a key; admin plane not published"

# SIGHUP, as in the runbook.
ct_compose "$p" kill -s HUP llm-proxy
i=0
until ct_admin "$p" http://127.0.0.1:9090/metrics | grep -q '^llm_proxy_config_reloads_total{result="success"} 1$'; do
	i=$((i + 1))
	[ "$i" -le 15 ] || {
		echo "FAIL SIGHUP did not reload the config" >&2
		exit 1
	}
	sleep 1
done
echo "ok  SIGHUP reloaded the config"
echo "compose test passed"
