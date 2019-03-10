NAME      := mogura
VERSION   := 0.1.1
HASH      := $(shell git rev-parse --short HEAD)
GOVERSION := $(shell go version)

LDFLAGS := -ldflags="-X \"main.version=$(VERSION)\" -X \"main.hash=$(HASH)\" -X \"main.goversion=$(GOVERSION)\""

.PHONY: build
build:
	export GO111MODULE=on
	ENABLED_CGO=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(NAME)-linux-amd64

.PHONY: mac-build
mac-build:
	export GO111MODULE=on
	ENABLED_CGO=0 GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(NAME)-darwin-amd64

.PHONY: win-build
win-build:
	export GO111MODULE=on
	ENABLED_CGO=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(NAME)-windows-amd64

.PHONY: docker-build
docker-build:
	docker build -t $(NAME):$(VERSION) .
