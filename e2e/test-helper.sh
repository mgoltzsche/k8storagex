#!/bin/sh

cd "$(dirname "$0"/..)"

IMAGE="${IMAGE:-cache-provisioner}"

VOLDIR=pv-xyz1_test-namespace_pvc-xyz
EXPECTED_CONTENT="testcontent $(date)"

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
		-e KUBE_CACHE_REGISTRY=$TEST_REGISTRY \
		-e KUBE_CACHE_REGISTRIES_CONF_PATH=/registries-config.json \
		-e KUBE_CACHE_REGISTRY_USERNAME=test \
		-e KUBE_CACHE_REGISTRY_PASSWORD=test \
		--mount "type=bind,source=`pwd`/e2e/registries-config.json,target=/registries-config.json" \
		--mount "type=bind,source=`pwd`/e2e/script,target=/script" \
		--mount "type=bind,source=`pwd`/testmount,target=/data,bind-propagation=rshared" \
		--entrypoint=/bin/sh \
		"$IMAGE" \
		"$@"
}

set -e

mkdir -p testmount
rm -rf testmount/$VOLDIR

echo
echo TEST setup $TEST_REGISTRY
echo
(
	set -ex
	runScript /script/setup
	echo "$EXPECTED_CONTENT" > testmount/$VOLDIR/testfile
	ls -la testmount/$VOLDIR
)

echo
echo TEST teardown $TEST_REGISTRY
echo
(
	set -ex
	runScript /script/teardown

	[ ! -d testmount/$VOLDIR ] || (echo fail: volume should be removed >&2; false)
)

if [ "$TEST_REGISTRY" ]; then
	echo
	echo deleting local storage
	echo
	docker run --rm --privileged --mount "type=bind,src=`pwd`,dst=/data" \
		alpine:3.12 /bin/sh -c '
			umount /data/testmount/.cache/overlay;
			rm -rf /data/testmount' || exit 1
fi

echo
echo TEST restore $TEST_REGISTRY
echo
(
	set -ex
	VOLDIR=pv-xyz2_test-namespace_pvc-xyz

	runScript /script/setup

	CONTENT="$(cat testmount/$VOLDIR/testfile)"
	[ "$CONTENT" = "$EXPECTED_CONTENT" ] || (echo fail: volume should return what was last written into that cache key >&2; false)

	runScript /script/teardown
)

#echo
#echo TEST prune
#echo
#(
#	set -ex
#	runScript -c 'buildah() { /usr/bin/buildah --root=/data/.cache/containers/storage "$@"; }; set -ex; buildah from --name c1 scratch; buildah commit c1; buildah rm c1'
#	runScript /usr/bin/cache-provisioner prune
#)
