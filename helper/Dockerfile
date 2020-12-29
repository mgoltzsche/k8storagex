ARG BUILDAH_VERSION=v1.18.0

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
	go build -o /usr/bin/buildah -ldflags "$VERSION_FLAGS -s -w -extldflags '-static'" -tags 'exclude_graphdriver_devicemapper containers_image_ostree_stub containers_image_openpgp' ./cmd/buildah

FROM alpine:3.12
ENV TZ=GMT
RUN apk add --update --no-cache tzdata
COPY policy.json /etc/containers/policy.json
COPY --from=buildah /usr/bin/buildah /usr/bin/buildah
COPY cache-provisioner.sh /usr/bin/cache-provisioner
ENTRYPOINT ["/usr/bin/cache-provisioner"]
