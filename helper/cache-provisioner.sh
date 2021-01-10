#!/bin/sh

set -eu

CACHE_ROOT_DIR="${CACHE_ROOT_DIR:-/data}"
CACHE_DIR="$CACHE_ROOT_DIR/.cache"
export STORAGE_DRIVER=overlay

usage() {
	echo "Usage: $0 prune|{mount|umount} CACHE_NAME DEST_DIR" >&2
	exit 1
}

# Args: NAME VALUE
validate() {
	PATTERN='^[-_a-z0-9]+$'
	echo "$2" | grep -Eq "$PATTERN" \
		|| (echo "invalid $1 argument provided: $2 (must match $PATTERN)" >&2; false)
}

buildah() {
	/usr/bin/buildah --log-level debug --root=$CACHE_DIR/containers/storage "$@"
}

# Args: CACHE_NAME
cacheImageName() {
	echo "localhost/cache/$1"
}

setupSharedCache() {
	mkdir -p "$CACHE_DIR/containers/storage"
}

# Mounts a cache image as volume directory.
# Args: CACHE_NAME MOUNT_NAME
mountCache() {
	validate CACHE_NAME "$1"
	validate MOUNT_NAME "$2"
	CACHE_NAME="$1"
	MOUNT_NAME="$2"
	VOL_DIR="$CACHE_ROOT_DIR/$MOUNT_NAME"
	setupSharedCache
	echo "Creating volume $VOL_DIR from cache '$CACHE_NAME'" >&2
	mkdir -m 0777 "$VOL_DIR" || exit 2
	(
		# Create new volume from cache's latest container image
		# (The latest cache image could be pulled from a registry here)
		(buildah from --pull-never --name "$MOUNT_NAME" "$(cacheImageName "$CACHE_NAME")" \
			|| ([ $? -eq 125 ] && (
				echo "Creating new cache image for '$CACHE_NAME'" >&2
				buildah from --name "$MOUNT_NAME" scratch
		))) >/dev/null &&
		CONTAINERDIR="$(buildah mount "$MOUNT_NAME")" &&
		mount -o bind,rshared "$CONTAINERDIR" "$VOL_DIR" &&
		chmod 0777 "$VOL_DIR"
	) || (
		umount "$VOL_DIR" 2>/dev/null 1>&2
		buildah umount "$MOUNT_NAME" 2>/dev/null 1>&2
		buildah delete "$MOUNT_NAME" 2>/dev/null 1>&2
		rm -rf "$VOL_DIR"
		false
	)
	echo "$VOL_DIR"
}

# Unmounts a cache volume directory, commits it and tags it as latest base image for the given CACHE_NAME.
# Args: CACHE_NAME MOUNT_NAME
umountCache() {
	validate CACHE_NAME "$1"
	validate MOUNT_NAME "$2"
	CACHE_NAME="$1"
	MOUNT_NAME="$2"
	VOL_DIR="$CACHE_ROOT_DIR/$MOUNT_NAME"
	setupSharedCache
	# Commit volume only if dir is mounted (node restart results in unmounted volumes).
	if mountpoint -q "$VOL_DIR"; then
		echo "Committing volume $VOL_DIR to cache '$CACHE_NAME'" >&2
		IMGID="$(buildah commit -q --timestamp 1 "$MOUNT_NAME")" &&
		buildah tag "$IMGID" "$(cacheImageName "$CACHE_NAME")" &&
		# The latest cache image could be pushed to a registry here
		umount "$VOL_DIR"
	fi

	# Delete volume / container
	echo "Deleting volume $VOL_DIR" >&2
	buildah umount "$MOUNT_NAME" >/dev/null || true
	buildah delete "$MOUNT_NAME" >/dev/null || true
	rm -rf "$VOL_DIR" || (printf 'error: volume deletion blocked by mount: '; grep $MOUNT_NAME /etc/mtab; false) >&2
}

pruneImages() {
	set -eux
	#buildah images
	#buildah version
	#buildah ps -a
	#buildah --log-level debug rmi --prune --force ||
	#podman --root=$CACHE_DIR/containers/storage image prune --force
	buildah rmi --prune --force
}

case "${1:-}" in
	mount)
		[ $# -eq 3 ] || usage
		mountCache "$2" "$3" || exit 3
	;;
	umount)
		[ $# -eq 3 ] || usage
		umountCache "$2" "$3" || exit 3
	;;
	prune)
		[ $# -eq 1 ] || (echo no arguments expected >&2; false) || usage
		pruneImages || exit 3
	;;
	*)
		usage
	;;
esac
