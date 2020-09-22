#!/bin/sh

set -e

ROOT_DIR="${ROOT_DIR:-/data}"
CACHE_DIR="${CACHE_DIR:-/data/.cache}"
CACHE_KEY_DIR="$CACHE_DIR/keys"

usage() {
	echo "Usage: $0 {mount|umount} INSTANCE_NAME [CACHE_KEY]"
	exit 1
}

# Args: INSTANCE_NAME [CACHE_KEY]
cacheKey() {
	validateName "$1"
	# derive cache key from instance name
	[ "$2" ] && validateName "$2" && echo "$2" && return 0
	DERIVED="$(echo "$1" | sed -E 's/pvc-[^_]+_([^_]+_.+)-[^-]+$/\1/')"
	[ ! "$DERIVED" = "$1" ] || (echo "cannot derive CACHE_KEY from $1" >&2; false)
	echo "$DERIVED"
}

# Args: NAME
validateName() {
	PATTERN='^[-_a-z0-9]+$'
	echo "$1" | grep -Eq "$PATTERN" \
		|| (echo "invalid argument provided: $1 (must match $PATTERN)" >&2; false)
}

setupSharedCache() {
	mkdir -p "$CACHE_KEY_DIR" &&
	mkdir -p "$CACHE_DIR/containers" &&
	cat - > /etc/containers/containers.conf <<-EOF
		[engine]
		cgroup_manager = "cgroupfs"
		events_logger = "file"
		static_dir = "$CACHE_DIR/containers/storage/libpod"
		volume_path = "$CACHE_DIR/containers/storage/volumes"
	EOF
	[ $? -eq 0 ] &&
	cat - > /etc/containers/storage.conf <<-EOF
		[storage]
		graphroot = "$CACHE_DIR/containers/storage"
		driver = "overlay"
		# disallow creating device nodes to avoid security risk
		# limit container size to 10gb (supported only on xfs and btrfs)
		mountopt = "nodev,size=10G"
	EOF
}

# Args: INSTANCE_NAME [CACHE_KEY]
mountCache() {
	CACHE_KEY="$(cacheKey "$1" "$2")" || exit 1
	CACHE_KEY_POINTER="$CACHE_KEY_DIR/$CACHE_KEY.latest"
	CACHE_MODE="${CACHE_MODE:-0777}"
	VOLNAME="$1"
	VOLPATH="$ROOT_DIR/$1"

	mkdir -m "$CACHE_MODE" "$VOLPATH" || exit 2
	(
		setupSharedCache &&
		# Create new volume from cache key's latest container image
		LATESTIMG="$(cat "$CACHE_KEY_POINTER" 2>/dev/null || echo scratch)" &&
		buildah from --name "$VOLNAME" $LATESTIMG &&
		CONTAINERDIR="$(buildah mount "$VOLNAME")" &&
		mount -o bind,rshared "$CONTAINERDIR" "$VOLPATH" &&
		chmod "$CACHE_MODE" "$VOLPATH"
	) || (
		umount "$VOLPATH" 2>/dev/null
		buildah umount "$VOLNAME" 2>/dev/null
		buildah delete "$VOLNAME" 2>/dev/null
		rm -rf "$VOLPATH"
		false
	)
}

# Args: INSTANCE_NAME [CACHE_KEY]
umountCache() {
	CACHE_KEY="$(cacheKey "$1" "$2")" || exit 1
	CACHE_KEY_POINTER="$CACHE_KEY_DIR/$CACHE_KEY.latest"
	VOLNAME="$1"
	VOLPATH="$ROOT_DIR/$1"
	
	setupSharedCache &&
	# Commit volume / container as latest cache key
	IMGID="$(buildah commit "$VOLNAME")" && (
		mkdir -p $CACHE_KEY_DIR &&
		TMPFILE=$(mktemp -p $CACHE_KEY_DIR) &&
		(echo "$IMGID" > $TMPFILE && mv $TMPFILE "$CACHE_KEY_POINTER" \
			|| (rm -f $TMPFILE; false))
		# TODO: push latest cache image to registry
	)

	# Delete volume / container
	umount "$VOLPATH"
	buildah umount "$VOLNAME"
	buildah delete "$VOLNAME"
	rm -rf "$VOLPATH"
}

case "$1" in
	mount)
		[ $# -le 3 ] || usage
		mountCache "$2" "$3" || exit 3
	;;
	umount)
		[ $# -le 3 ] || usage
		umountCache "$2" "$3" || exit 3
	;;
	*)
		usage
	;;
esac
