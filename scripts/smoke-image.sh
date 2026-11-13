#!/bin/sh
# Smoke test for the container image (ADR 0036). Starts Postgres and the image,
# then checks /healthz and /readyz on the data plane and /ui/ on the admin
# plane. The admin plane listens on loopback inside the container, so the checks
# for it run in a curl container that shares the proxy's network namespace.
# Usage: smoke-image.sh IMAGE
set -eu

image=${1:?usage: smoke-image.sh IMAGE}
curl_image=${CURL_IMAGE:-curlimages/curl:8.11.1}
suffix=$$
net=spyglass-smoke-$suffix
pg=spyglass-smoke-pg-$suffix
app=spyglass-smoke-app-$suffix
tmp=$(mktemp -d)

cleanup() {
	status=$?
	if [ "$status" -ne 0 ]; then
		echo "--- spyglass logs ---" >&2
		docker logs "$app" >&2 2>&1 || true
	fi
	docker rm -f "$app" "$pg" >/dev/null 2>&1 || true
	docker network rm "$net" >/dev/null 2>&1 || true
	rm -rf "$tmp"
	exit "$status"
}
trap cleanup EXIT

# A hash that matches no real key. client_passthrough needs no provider secret.
cat >"$tmp/config.yaml" <<YAML
listen:
  data: ":8080"
  admin: "127.0.0.1:9090"
database:
  url: { env: SPYGLASS_DATABASE_URL }
providers:
  - name: anthropic
    kind: anthropic
    base_url: https://api.anthropic.com
    auth_mode: client_passthrough
keys:
  - name: smoke
    sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
YAML
# The nonroot user must be able to read the file.
chmod 644 "$tmp/config.yaml"

docker network create "$net" >/dev/null
docker run -d --name "$pg" --network "$net" \
	-e POSTGRES_PASSWORD=postgres \
	--health-cmd "pg_isready -U postgres" --health-interval 2s --health-retries 30 \
	postgres:16-alpine >/dev/null

echo "waiting for postgres"
i=0
until [ "$(docker inspect --format '{{.State.Health.Status}}' "$pg")" = healthy ]; do
	i=$((i + 1))
	[ "$i" -le 60 ] || {
		echo "postgres did not become healthy" >&2
		exit 1
	}
	sleep 1
done

# Read-only root filesystem: the container writes nothing to disk.
docker run -d --name "$app" --network "$net" --read-only \
	-e SPYGLASS_DATABASE_URL="postgres://postgres:postgres@$pg:5432/postgres?sslmode=disable" \
	-v "$tmp/config.yaml:/etc/spyglass/config.yaml:ro" \
	"$image" serve --config /etc/spyglass/config.yaml >/dev/null

# probe URL: fetch from inside the proxy's network namespace.
probe() {
	docker run --rm --network "container:$app" "$curl_image" -fsS -o /dev/null -w "%{http_code}" "$1" 2>/dev/null
}

check() {
	url=$1
	i=0
	while :; do
		if [ "$(probe "$url" || true)" = 200 ]; then
			echo "ok  $url"
			return 0
		fi
		i=$((i + 1))
		if [ "$i" -gt 30 ]; then
			echo "FAIL $url" >&2
			return 1
		fi
		sleep 1
	done
}

check http://127.0.0.1:8080/healthz
check http://127.0.0.1:8080/readyz
check http://127.0.0.1:9090/healthz
check http://127.0.0.1:9090/readyz
check http://127.0.0.1:9090/ui/

# The embedded UI, not the noui stub, must be served.
body=$(docker run --rm --network "container:$app" "$curl_image" -fsS http://127.0.0.1:9090/ui/)
case "$body" in
*'<div id="root"'*) echo "ok  /ui/ serves the built UI" ;;
*)
	echo "FAIL /ui/ did not serve the built UI" >&2
	exit 1
	;;
esac
echo "smoke test passed"
