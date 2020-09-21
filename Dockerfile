# TODO: base from alpine add buildah here with fake runc

ARG BUILDAH_VERSION=v1.16.1

FROM golang:1.14-alpine3.12 AS buildah
RUN apk add --update --no-cache bash git make gcc pkgconf musl-dev \
	glib-static libc-dev gpgme-dev libseccomp-dev libselinux-dev \
	btrfs-progs btrfs-progs-dev libassuan-dev lvm2-dev device-mapper ostree-dev
ARG BUILDAH_VERSION
RUN git clone --branch ${BUILDAH_VERSION} https://github.com/containers/buildah $GOPATH/src/github.com/containers/buildah
WORKDIR $GOPATH/src/github.com/containers/buildah
RUN set -ex; \
	GIT_COMMIT="`git rev-parse --short HEAD`"; \
	VERSION_FLAGS="-X main.GitCommit=$GIT_COMMIT -X main.buildInfo=`date +%s`"; \
	go build -o /usr/local/bin/buildah -ldflags "$VERSION_FLAGS -s -w -extldflags '-static'" -tags 'exclude_graphdriver_devicemapper containers_image_ostree_stub containers_image_openpgp' ./cmd/buildah

FROM alpine:3.12
ENV TZ=GMT
RUN apk add --update --no-cache tzdata
RUN mkdir /etc/containers
COPY policy.json /etc/containers/policy.json
COPY --from=buildah /usr/local/bin/buildah /usr/local/bin/buildah
COPY cache-provisioner.sh /bin/cache-provisioner
ENTRYPOINT ["/bin/cache-provisioner"]
