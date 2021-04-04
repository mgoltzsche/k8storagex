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

# ARGS: SCRIPT
runScript() {
	mkdir -p testmount
	docker run --rm --privileged --network=host \
		-e VOL_DIR=/data/$VOLDIR \
		-e VOL_NAME=pv-xyz \
		-e VOL_SIZE_BYTES=12345678 \
		-e PVC_NAME=pvc-xyz \
		-e PVC_NAMESPACE=test-namespace \
		-e PVC_ANNOTATION_CACHE_NAME=test-cache \
		-e DCOWFS_REGISTRY=$TEST_REGISTRY \
		-e DCOWFS_REGISTRY_USERNAME=testuser \
		-e DCOWFS_REGISTRY_PASSWORD=testpass \
		-e DCOWFS_REGISTRIES_CONF_PATH=/registries-config.json \
		-e DCOWFS_INSECURE_SKIP_TLS_VERIFY=true \
		--mount "type=bind,source=`pwd`/registries-config.json,target=/registries-config.json" \
		--mount "type=bind,source=`pwd`/script,target=/script" \
		--mount "type=bind,source=`pwd`/testmount,target=/data,bind-propagation=rshared" \
		--entrypoint=/bin/sh \
		"$IMAGE" \
		"$@"
}

@test "setup should create volume [$TEST_REGISTRY]" {
	runScript /script/setup
	echo "$EXPECTED_CONTENT" > testmount/$VOLDIR/testfile
	ls -la testmount/$VOLDIR
}

@test "teardown should remove volume [$TEST_REGISTRY]" {
	runScript /script/teardown
	[ ! -d testmount/$VOLDIR ] || (echo fail: volume should be removed >&2; false)
}

@test "subsequent setup should restore volume [$TEST_REGISTRY]" {
	if [ "$TEST_REGISTRY" ]; then
		# Delete local storage when testing against a registry
		docker run --rm --privileged --mount "type=bind,src=`pwd`,dst=/data" \
			alpine:3.12 /bin/sh -c '
				umount /data/testmount/.cache/overlay;
				rm -rf /data/testmount' || exit 1
	fi

	VOLDIR=pv-xyz2_test-namespace_pvc-xyz

	runScript /script/setup

	CONTENT="$(cat testmount/$VOLDIR/testfile)"
	[ "$CONTENT" = "$EXPECTED_CONTENT" ] || (echo fail: volume should return what was last written into that cache key >&2; false)

	runScript /script/teardown
}
