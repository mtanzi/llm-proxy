# Shared helpers for compose-test.sh and restore-test.sh (ADR 0037). Source it;
# do not run it. It builds a throwaway deployment from the files a user would
# copy (config.example.yaml and the secrets), points it at a fake upstream, and
# drives it with the real docker-compose.yml.

repo=$(cd "$(dirname "$0")/.." && pwd)
curl_image=${CURL_IMAGE:-curlimages/curl:8.11.1}
port=${LLM_PROXY_PORT:-18080}
tmp=$(mktemp -d)
projects=""

# ct_compose PROJECT ARGS...: the repository's compose file plus the fake upstream.
ct_compose() {
	project=$1
	shift
	LLM_PROXY_IMAGE=$image LLM_PROXY_CONFIG_FILE=$tmp/config.yaml LLM_PROXY_SECRETS_DIR=$tmp/secrets \
		LLM_PROXY_PORT=$port docker compose -p "$project" \
		-f "$repo/docker-compose.yml" -f "$repo/testdata/compose/docker-compose.fake-upstream.yml" "$@"
}

ct_cleanup() {
	status=$?
	if [ "$status" -ne 0 ]; then
		for p in $projects; do
			echo "--- logs of $p ---" >&2
			ct_compose "$p" logs --no-color --tail 80 >&2 2>&1 || true
		done
	fi
	for p in $projects; do
		ct_compose "$p" down -v --timeout 5 >/dev/null 2>&1 || true
	done
	rm -rf "$tmp"
	exit "$status"
}

# ct_setup IMAGE: write config.yaml and the secret files into a temp directory.
# It creates a key with the image's own keygen, as the runbook does, and puts its
# hash into a copy of config.example.yaml. $key holds the plaintext key.
ct_setup() {
	image=$1
	trap ct_cleanup EXIT
	mkdir -p "$tmp/secrets"
	chmod 755 "$tmp" "$tmp/secrets"
	pw=$(openssl rand -hex 24)
	printf '%s\n' "$pw" >"$tmp/secrets/pg_password"
	printf 'postgres://llmproxy:%s@postgres:5432/llmproxy?sslmode=disable\n' "$pw" >"$tmp/secrets/database_url"
	printf 'sk-ant-compose-test-placeholder\n' >"$tmp/secrets/anthropic_api_key"

	out=$(docker run --rm "$image" keygen --name my-laptop)
	key=$(printf '%s\n' "$out" | sed -n 's/^plaintext key .*: \(lp_[0-9A-Za-z]*\)$/\1/p')
	hash=$(printf '%s\n' "$out" | sed -n 's/^ *sha256: *\([0-9a-f]\{64\}\)$/\1/p')
	[ -n "$key" ] && [ -n "$hash" ] || {
		echo "could not read the key and hash from keygen output" >&2
		return 1
	}
	# The first placeholder hash is the my-laptop entry. The upstream is the fake.
	sed -e "s/^    sha256: 0\{63\}1 .*/    sha256: $hash/" \
		-e 's|base_url: https://api.anthropic.com|base_url: http://fake-upstream:8080|' \
		"$repo/config.example.yaml" >"$tmp/config.yaml"
	grep -q "$hash" "$tmp/config.yaml" || {
		echo "config.example.yaml no longer has the placeholder hash this test replaces" >&2
		return 1
	}
	# The distroless nonroot user (uid 65532) must be able to read the files.
	chmod 644 "$tmp/config.yaml" "$tmp/secrets/"*
}

# ct_wait_ready PROJECT: wait for Postgres to be healthy, start the stack, and
# wait until the data plane answers /readyz on the published port.
ct_start() {
	project=$1
	projects="$projects $project"
	ct_compose "$project" up -d --wait --wait-timeout 120 postgres fake-upstream
	ct_compose "$project" up -d llm-proxy
	i=0
	until [ "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/readyz")" = 200 ]; do
		i=$((i + 1))
		[ "$i" -le 60 ] || {
			echo "llm-proxy did not become ready" >&2
			return 1
		}
		sleep 1
	done
	echo "ok  stack $project is ready"
}

# ct_admin PROJECT ARGS...: curl on the admin plane from inside the container's
# network namespace (the plane listens on loopback there).
ct_admin() {
	project=$1
	shift
	cid=$(ct_compose "$project" ps -q llm-proxy)
	docker run --rm --network "container:$cid" "$curl_image" -sS "$@"
}

# ct_send: one non-streamed call through the data plane. Prints the status.
ct_send() {
	curl -s -o "$tmp/reply.json" -w '%{http_code}' "http://127.0.0.1:$port/v1/messages" \
		-H "Authorization: Bearer $key" -H 'anthropic-version: 2023-06-01' \
		-H 'content-type: application/json' \
		-d '{"model":"claude-opus-4-8","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}'
}

# ct_wait_call PROJECT: wait until the call log holds a call; sets $call_id.
ct_wait_call() {
	i=0
	while :; do
		body=$(ct_admin "$1" http://127.0.0.1:9090/admin/calls || true)
		call_id=$(printf '%s' "$body" | sed -n 's/.*"calls":\[{"id":"\([0-9a-f-]*\)".*/\1/p')
		[ -z "$call_id" ] || return 0
		i=$((i + 1))
		[ "$i" -le 30 ] || {
			echo "no call in the call log: $body" >&2
			return 1
		}
		sleep 1
	done
}

# ct_check_ui PROJECT: the embedded UI, not the noui stub, is served.
ct_check_ui() {
	case $(ct_admin "$1" -f http://127.0.0.1:9090/ui/) in
	*'<div id="root"'*) echo "ok  /ui/ serves the built UI" ;;
	*)
		echo "FAIL /ui/ did not serve the built UI" >&2
		return 1
		;;
	esac
}
