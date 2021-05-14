#!/usr/bin/bash
set -euo pipefail

# this script must be run as root
if [ $UID != 0 ]; then
  echo This script must be run as root.
  exit 1
fi

CA_DIR="/etc/otto"
echo "Generating certificates"
mkdir -p ${CA_DIR}

CONFIG="openssl.conf"

# The CA
echo "-=[ CA"
touch "${CA_DIR}/index.txt"
openssl req -new -nodes -x509 \
        -config "${CONFIG}" \
        -extensions osbuild_ca_ext \
        -keyout "${CA_DIR}/ca-key.pem" \
        -out "${CA_DIR}/ca-crt.pem" \
        -subj "/CN=osbuild.org"

# server
echo "-=[ server"
openssl genrsa -out ${CA_DIR}/server-key.pem 2048
openssl req -new -sha256 \
        -config "${CONFIG}" \
        -key ${CA_DIR}/server-key.pem	\
        -out ${CA_DIR}/server-csr.pem \
        -subj "/CN=otto" \
        -addext "subjectAltName=DNS.1:localhost,DNS.2:otto"

openssl ca -config "$CONFIG" -batch \
        -extensions osbuild_server_ext \
        -in "${CA_DIR}/server-csr.pem" \
        -out "${CA_DIR}/server-crt.pem"

echo "-=[ Updating system trust chain"
cp ${CA_DIR}/ca-crt.pem \
   /etc/pki/ca-trust/source/anchors/otto-ca-crt.pem

update-ca-trust