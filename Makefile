IMAGE=mgoltzsche/cache-provisioner-helper:latest

all: image

image:
	docker build --force-rm -t "$(IMAGE)" helper

test: image
	IMAGE=$(IMAGE) ./test-helper.sh

clean:
	docker run --rm --privileged -v `pwd`:/data alpine:3.12 /bin/sh -c ' \
		umount /data/testmount/*; \
		umount /data/testmount/.cache/containers/storage/overlay; \
		rm -rf /data/testmount'
