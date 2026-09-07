#!/bin/bash
set -e

SERVICE=${1:-k8s-dns-admission-controller.default.svc}
DIR=${2:-certs}

command -v openssl >/dev/null 2>&1 || { echo >&2 "openssl is required but not installed. Aborting."; exit 1; }

mkdir -p ${DIR}

openssl genrsa -out ${DIR}/ca.key 2048
openssl req -x509 -new -nodes -key ${DIR}/ca.key -subj "/CN=${SERVICE}" -days 10000 -out ${DIR}/ca.crt

openssl genrsa -out ${DIR}/tls.key 2048
openssl req -new -key ${DIR}/tls.key -out ${DIR}/tls.csr -config <(cat <<EOF
[req]
default_bits = 2048
prompt = no
default_md = sha256
req_extensions = req_ext
distinguished_name = dn

[dn]
CN = ${SERVICE}

[req_ext]
subjectAltName = @alt_names

[alt_names]
DNS.1 = ${SERVICE}
EOF
)

openssl x509 -req -in ${DIR}/tls.csr -CA ${DIR}/ca.crt -CAkey ${DIR}/ca.key -CAcreateserial -out ${DIR}/tls.crt -days 10000 -extensions req_ext -extfile <(cat <<EOF
[req_ext]
subjectAltName = @alt_names

[alt_names]
DNS.1 = ${SERVICE}
EOF
)
