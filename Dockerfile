FROM --platform=$BUILDPLATFORM golang:1.25.4 AS builder
ARG TARGETARCH

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN GOARCH=${TARGETARCH} go build -ldflags="-s -w" -trimpath -o longhorn-external-share-manager .

FROM alpine:3.23

COPY --from=builder /build/longhorn-external-share-manager  /bin/longhorn-external-share-manager
ENTRYPOINT [ "/bin/longhorn-external-share-manager" ]
