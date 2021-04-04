#!/bin/sh

cd "$(dirname "$0")"

echo
echo RUNNING LAYERFS CONTAINER TESTS WITHOUT REGISTRY...
echo

IMAGE=$IMAGE bats -T tests.bats || exit 1

echo
echo RUNNING LAYERFS CONTAINER TESTS USING A REGISTRY...
echo

set -eu

TLS_CERT_DIR="`pwd`/fake-tls-cert"

if [ ! -d "$TLS_CERT_DIR" ]; then
	echo Creating test SSL certificate...
	mkdir "$TLS_CERT_DIR"
	openssl req -x509 -nodes -subj "/CN=fake" -newkey rsa:2048 -keyout "$TLS_CERT_DIR/key.pem" -out "$TLS_CERT_DIR/cert.pem" -days 365 2>/dev/null || (rm -rf "$TLS_CERT_DIR"; false)
fi

REGISTRY_NAME=cache-test-registry
echo Launching test container registry ${REGISTRY_NAME}...
docker rm -f $REGISTRY_NAME 2>/dev/null || true
docker run -d --rm --name $REGISTRY_NAME --network=host \
	-e REGISTRY_HTTP_ADDR=:5000 \
	-e REGISTRY_HTTP_TLS_CERTIFICATE=/tls/cert.pem \
	-e REGISTRY_HTTP_TLS_KEY=/tls/key.pem \
	-e REGISTRY_AUTH=htpasswd \
	-e REGISTRY_AUTH_HTPASSWD_REALM=test-realm \
	-e REGISTRY_AUTH_HTPASSWD_PATH=/htpasswd \
	--mount "type=bind,src=`pwd`/fake-htpasswd,dst=/htpasswd,readonly" \
	--mount "type=bind,src=$TLS_CERT_DIR,dst=/tls,readonly" \
	registry:2.7 >/dev/null


sleep 7
echo

IMAGE=$IMAGE TEST_REGISTRY=docker://127.0.0.1:5000 bats -T tests.bats

docker rm -f $REGISTRY_NAME >/dev/null || true
