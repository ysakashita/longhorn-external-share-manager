IMG := ysakashita/longhorn-external-share-manager
TAG := dev
GOOS := linux
GOARCH := arm64
PLATFORM := $(GOOS)/$(GOARCH)

.PHONY: build
build:
	CGO_ENABLED=0 GOARCH=$(GOARCH) GOOS=$(GOOS) go build -o _out/longhorn-external-share-manager

.PHONY: build-image
build-image: 
	docker buildx build --platform $(PLATFORM) -t $(IMG):$(TAG) --load .

.PHONY: push-image
push-image: build-image
	docker push $(IMG):$(TAG)

.PHONY: lint
lint:
	golangci-lint run

.PHONY: clean
clean:
	rm -rf _out
