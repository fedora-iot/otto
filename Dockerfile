FROM registry.fedoraproject.org/fedora-minimal AS builder
RUN mkdir /workspace && microdnf -y install golang && microdnf -y clean all
WORKDIR /workspace
COPY . .
RUN GOBIN=/usr/local/bin/ go install -v ./cmd/otto/

FROM registry.fedoraproject.org/fedora-minimal
RUN microdnf install -y ostree tar
COPY --from=builder /usr/local/bin/otto /usr/libexec/otto/

EXPOSE 3000
CMD ["/usr/libexec/otto/otto"]
