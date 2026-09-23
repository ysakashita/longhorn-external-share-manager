IMG := ysakashita/longhorn-external-share-manager
SMB_IMG := ysakashita/longhorn-external-share-manager-smb-gateway
TAG := dev
GOOS := linux
GOARCH := arm64
PLATFORM := $(GOOS)/$(GOARCH)

.PHONY: build
build:
	CGO_ENABLED=0 GOARCH=$(GOARCH) GOOS=$(GOOS) go build -o _out/longhorn-external-share-manager

.PHONY: build-image
build-image:
	docker buildx build --platform $(PLATFORM) -f images/controller/Dockerfile -t $(IMG):$(TAG) --load .

.PHONY: push-image
push-image: build-image
	docker push $(IMG):$(TAG)

.PHONY: build-smb-image
build-smb-image:
	docker buildx build --platform $(PLATFORM) -t $(SMB_IMG):$(TAG) --load images/smb-gateway

.PHONY: push-smb-image
push-smb-image: build-smb-image
	docker push $(SMB_IMG):$(TAG)

.PHONY: lint
lint:
	golangci-lint run

.PHONY: clean
clean:
	rm -rf _out
