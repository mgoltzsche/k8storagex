#!/bin/sh

set -eux

#IMAGE=$IMAGE ./e2e/test-helper.sh

DIR=`pwd`/e2e
TLS_CERT_DIR="$DIR/fake-tls-cert"

if [ ! -d "$TLS_CERT_DIR" ]; then
	mkdir "$TLS_CERT_DIR"
	openssl req -x509 -nodes -subj "/CN=fake" -newkey rsa:2048 -keyout "$TLS_CERT_DIR/key.pem" -out "$TLS_CERT_DIR/cert.pem" -days 365 || (rm -rf "$TLS_CERT_DIR"; false)
fi

REGISTRY_NAME=cache-test-registry
docker rm -f $REGISTRY_NAME || true
docker run -d --rm --name $REGISTRY_NAME --network host \
	-e REGISTRY_HTTP_ADDR=127.0.0.1:8989 \
	-e REGISTRY_HTTP_HOST=http://127.0.0.1:8989 \
	-e REGISTRY_HTTP_RELATIVEURLS=true \
	-e REGISTRY_AUTH=htpasswd \
	-e REGISTRY_AUTH_HTPASSWD_REALM=test-realm \
	-e REGISTRY_AUTH_HTPASSWD_PATH=/htpasswd \
	-e REGISTRY_AUTH_TOKEN_ROOTCERTBUNDLE=/tls-cert \
	--mount "type=bind,src=$TLS_CERT_DIR,dst=/tls-cert,readonly" \
	--mount "type=bind,src=`pwd`/e2e/fake-htpasswd,dst=/htpasswd,readonly" \
	registry:2.7

sleep 5

IMAGE=$IMAGE TEST_REGISTRY=docker://127.0.0.1:8989 ./e2e/test-helper.sh

docker rm -f $REGISTRY_NAME || true
