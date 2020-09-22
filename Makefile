IMAGE=cache-provisioner

all: image

image:
	docker build --force-rm -t "$(IMAGE)" helper

test: image
	./test.sh

clean:
	umount testdata/pvc-xyz_default_build-cache || true
	umount testdata/.cache/containers/storage/overlay || true
	rm -rf testdata || true
