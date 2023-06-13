# build the plugin
FROM golang:1.20 as builder
ARG TARGETOS
ARG TARGETARCH
ARG APPVERSION=dev
WORKDIR /src

# copy the go modules manifests and sums
COPY go.mod go.mod
COPY go.sum go.sum

# this helps caching the dependencies and they don't need to be rebuilt all the time
RUN go mod download

# Copy the go source
COPY cmd/ cmd/
COPY internal/ internal/
COPY pkg/ pkg/

# now build a static go binary
WORKDIR /src/cmd/k8s-tpm-device-plugin
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -a -ldflags="-w -s -X 'go.githedgehog.com/k8s-tpm-device-plugin/pkg/version.Version=${APPVERSION}'" .

# use distroless as minimal base image which is ideal for static go binaries
FROM gcr.io/distroless/static-debian11:latest
WORKDIR /tmp
COPY --from=builder /src/cmd/k8s-tpm-device-plugin/k8s-tpm-device-plugin /bin/k8s-tpm-device-plugin
ENTRYPOINT ["/bin/k8s-tpm-device-plugin"]

LABEL org.opencontainers.image.authors="Marcus Heese <marcus@githedgehog.com>"
LABEL org.opencontainers.image.version="$APPVERSION"
LABEL org.opencontainers.image.title="Kubernetes TPM Device Plugin"
LABEL org.opencontainers.image.description="A simple Kubernetes device plugin that allows access to the TPM device of a host"
LABEL org.opencontainers.image.url="https://github.com/githedgehog/k8s-tpm-device-plugin/"
LABEL org.opencontainers.image.source="https://github.com/githedgehog/k8s-tpm-device-plugin/"
LABEL org.opencontainers.image.licenses="Apache-2.0"
LABEL org.opencontainers.image.vendor="Hedgehog SONiC Foundation"
