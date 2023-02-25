NAME      := mogura
VERSION   := 0.1.4
HASH      := $(shell git rev-parse --short HEAD)
GOVERSION := $(shell go version)

LDFLAGS := -ldflags="-X \"main.version=$(VERSION)\" -X \"main.hash=$(HASH)\" -X \"main.goversion=$(GOVERSION)\""

.PHONY: build
build: linux-build mac-build win-build

.PHONY: linux-build
linux-build:
	export GO111MODULE=on
	ENABLED_CGO=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(NAME)
	zip -j bin/$(NAME)-$(VERSION)-linux-amd64.zip bin/$(NAME)

.PHONY: mac-build
mac-build:
	export GO111MODULE=on
	ENABLED_CGO=0 GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(NAME)
	zip -j bin/$(NAME)-$(VERSION)-darwin-amd64.zip bin/$(NAME)

.PHONY: win-build
win-build:
	export GO111MODULE=on
	ENABLED_CGO=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(NAME)
	zip -j bin/$(NAME)-$(VERSION)-windows-amd64.zip bin/$(NAME)

.PHONY: docker-build
docker-build:
	docker build -t $(NAME):$(VERSION) .

.PHONY: test-goreleaser
test-goreleaser:
	goreleaser --snapshot --skip-publish --clean

.PHONY: build-test-docker-image
build-test-docker-image:
	docker build -f local-env/Dockerfile.ssh-server -t mogura-bastion:0.1.0 .

.PHONY: launch-test-servers
launch-test-servers: build-test-docker-image
	docker-compose -f local-env/docker-compose.yml up

.PHONY: launch-test-mogura
launch-test-mogura:
	bin/mogura -config config.yml.sample
