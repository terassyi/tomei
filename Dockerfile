FROM ubuntu:24.04

ARG TARGETARCH
ARG GO_VERSION=1.26.0

ENV DEBIAN_FRONTEND=noninteractive
ENV GOPATH=/go
ENV PATH=/go/bin:/usr/local/go/bin:$PATH

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    git \
    make \
    && rm -rf /var/lib/apt/lists/*

RUN curl -sfL https://dl.google.com/go/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz \
    | tar -x -z -C /usr/local -f - \
    && mkdir -p /go/src \
    && GOBIN=/usr/local/bin go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest \
    && rm -rf /go \
    && mkdir -p /go/src

WORKDIR /workspace

CMD ["bash"]
