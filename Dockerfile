FROM --platform=$BUILDPLATFORM golang:1.21.6 as builder
ARG TARGETARCH

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN GOARCH=${TARGETARCH} go build -ldflags="-s -w" -trimpath -o longhorn-external-share-manager .

FROM ubuntu:22.04

COPY --from=builder /build/longhorn-external-share-manager  /bin/longhorn-external-share-manager
ENTRYPOINT [ "/bin/longhorn-external-share-manager" ]
