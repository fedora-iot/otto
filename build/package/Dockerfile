FROM registry.fedoraproject.org/fedora-minimal AS builder
RUN mkdir /workspace && microdnf -y install golang openssl && microdnf -y clean all
WORKDIR /workspace

COPY ./build/package/make-certs.sh .
COPY ./build/package/openssl.conf .

RUN ./make-certs.sh

COPY ./go.mod ./go.sum .
RUN go mod download

COPY . .
RUN GOBIN=/usr/local/bin/ go install -v ./cmd/otto/

FROM registry.fedoraproject.org/fedora-minimal
RUN microdnf install -y ostree tar
COPY --from=builder /usr/local/bin/otto /usr/libexec/otto/

RUN mkdir -p /etc/otto
COPY --from=builder /etc/otto /etc/otto
RUN ln /etc/otto/ca-crt.pem /etc/pki/ca-trust/source/anchors/otto-ca-crt.pem
RUN update-ca-trust

EXPOSE 3000
CMD ["/usr/libexec/otto/otto"]
