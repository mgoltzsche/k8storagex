#!/bin/sh

[ $# -eq 2 ] || (echo "Usage: $0 {setup|teardown} DESTDIR" >&2; false)

docker run --rm --privileged \
	-v `pwd`/config:/config \
	--mount type=bind,source=`pwd`/testdata,target=/data,bind-propagation=rshared \
	--entrypoint=/bin/sh \
	mgoltzsche/podman:1.9.3 \
	"/config/$1" "/data/$2"