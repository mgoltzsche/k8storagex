#!/usr/bin/env bats

IMAGE="${IMAGE:-cache-provisioner}"
VOLDIR=pv-xyz1_test-namespace_pvc-xyz
EXPECTED_CONTENT="testcontent"

setup() {
	# emulate the upcoming bats `setup_file`
	# https://github.com/bats-core/bats-core/issues/39#issuecomment-377015447
	if [ $BATS_TEST_NUMBER -eq 1 ]; then
		mkdir -p testmount
		rm -rf testmount/$VOLDIR
	fi
}

# ARGS: COMMAND
runContainer() {
	mkdir -p testmount
	docker run --rm --privileged --network=host \
		-e LAYERFS_NAME="test-cache" \
		-e LAYERFS_NAMESPACE=test-namespace \
		-e LAYERFS_CONTAINER_NAME=$VOLDIR \
		-e LAYERFS_STORAGE_ROOT=/data/.cache \
		-e LAYERFS_REGISTRY=$TEST_REGISTRY \
		-e LAYERFS_REGISTRY_USERNAME=testuser \
		-e LAYERFS_REGISTRY_PASSWORD=testpass \
		-e LAYERFS_REGISTRIES_CONF_PATH=/registries-config.json \
		-e LAYERFS_INSECURE_SKIP_TLS_VERIFY=true \
		--mount "type=bind,source=`pwd`/registries-config.json,target=/registries-config.json" \
		--mount "type=bind,source=`pwd`/script,target=/script" \
		--mount "type=bind,source=`pwd`/testmount,target=/data,bind-propagation=rshared" \
		"$IMAGE" \
		"$@"
}

@test "mount should create writeable dir [$TEST_REGISTRY]" {
	runContainer layerfs mount /data/$VOLDIR --mode=0777
	echo "$EXPECTED_CONTENT" > testmount/$VOLDIR/testfile
	ls -la testmount/$VOLDIR
}

@test "umount should remove volume dir [$TEST_REGISTRY]" {
	runContainer layerfs umount /data/$VOLDIR --commit
	[ ! -d testmount/$VOLDIR ] || (echo fail: volume should be removed >&2; false)
}

@test "mount should restore previous volume contents [$TEST_REGISTRY]" {
	if [ "$TEST_REGISTRY" ]; then
		# Delete local storage when testing against a registry
		docker run --rm --privileged --mount "type=bind,src=`pwd`,dst=/data" \
			alpine:3.12 /bin/sh -c '
				umount /data/testmount/.cache/overlay;
				rm -rf /data/testmount' || exit 1
	fi

	VOLDIR=pv-xyz2_test-namespace_pvc-xyz

	runContainer layerfs mount /data/$VOLDIR --mode=0777

	CONTENT="$(cat testmount/$VOLDIR/testfile)"
	[ "$CONTENT" = "$EXPECTED_CONTENT" ] || (echo fail: volume should return what was last written into that cache key >&2; false)

	runContainer layerfs umount /data/$VOLDIR --commit
}
