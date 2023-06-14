# Copyright 2023 Hedgehog SONiC Foundation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
# 
# 	http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
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
