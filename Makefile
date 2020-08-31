all: clean test

test:
	./test.sh

clean:
	umount testdata/pv1 || true
	umount testdata/.cache/containers/storage/overlay || true
	rm -rf testdata || true
