FROM registry.access.redhat.com/ubi8/go-toolset:latest AS builder
COPY . .
RUN go install ./cmd/otto/

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
RUN microdnf install ostree tar
COPY --from=builder /opt/app-root/src/go/bin/otto /usr/libexec/otto/

EXPOSE 8000
CMD ["/usr/libexec/otto/otto"]
