#!/bin/sh

[ $# -eq 3 ] || (echo "Usage: $0 {setup|teardown} ROOTDIR INSTANCENAME" >&2; false)

docker run --rm --privileged \
	-v `pwd`/deploy/config:/config \
	--mount "type=bind,source=$2,target=/data,bind-propagation=rshared" \
	--entrypoint=/bin/sh \
	cache-provisioner \
	"/config/$1" "/data/$3"
