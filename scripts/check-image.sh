#!/bin/sh
# Check a built image: it runs as the distroless nonroot user (uid 65532) and
# is no bigger than a size budget (ADR 0036). Usage: check-image.sh IMAGE
set -eu

image=${1:?usage: check-image.sh IMAGE}
max_bytes=${MAX_IMAGE_BYTES:-20971520} # 20 MiB; a build is about 6 MB with the UI

user=$(docker image inspect --format '{{.Config.User}}' "$image")
case "$user" in
65532:65532) ;;
*)
	echo "image runs as '$user', want 65532:65532 (nonroot)" >&2
	exit 1
	;;
esac

# The size docker reports for the image on this platform.
size=$(docker image inspect --format '{{.Size}}' "$image")
echo "image $image: user=$user size=$size bytes (budget $max_bytes)"
if [ "$size" -gt "$max_bytes" ]; then
	echo "image is $size bytes, over the $max_bytes budget" >&2
	exit 1
fi
