IMAGE=mgoltzsche/cache-provisioner-helper:latest

all: image

image:
	docker build --force-rm -t "$(IMAGE)" helper

test: image
	IMAGE=$(IMAGE) ./test-helper.sh

install-buildah: image
	CID=`docker create $(IMAGE)` && \
	docker cp $$CID:/usr/bin/buildah /usr/local/bin/buildah; \
	STATUS=$$?; \
	docker rm $$CID; \
	exit $$STATUS

clean:
	docker run --rm --privileged -v `pwd`:/data alpine:3.12 /bin/sh -c ' \
		umount /data/testmount/*; \
		umount /data/testmount/.cache/containers/storage/overlay; \
		rm -rf /data/testmount'
