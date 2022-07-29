my_dir := $(abspath $(shell dirname $(lastword $(MAKEFILE_LIST))))
version = $(shell git log -n 1 --date=short --pretty=format:%cs.%h)
commit = $(shell git log -n 1 --date=short --pretty=format:%h)
tag = $(shell git describe --abbrev=0 --tags)
_ldflags=-w -X main.version=${tag}~${commit} -X 'ddn.com/perms/pkg/kube.ddnModelVersion=${version}'
ldflags=$(_ldflags)
devldflags=$(_ldflags) -X main.developmentMode=1
export PATH := $(my_dir)/bin:$(shell readlink -f ../tools/bin):$(PATH)

PREFIX ?= /usr
DESTDIR ?=i
export CGO_ENABLED=0

build-%:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux \
		go build -ldflags "${ldflags}" -mod vendor -v -o ${my_dir}/bin/$* $*.go
	
.PHONY: clean
clean:
	@go clean .
	@rm -rf bin


.PHONY: go-deps
go-deps:
	go mod tidy
	go mod vendor
	go mod download

.PHONY: images
images:
	docker buildx build \
	--platform linux/arm64,linux/amd64 \
	--tag ghcr.io/darkmuggle/kubevirt-hooks:${commit} \
	--output "type=image,push=true" \
	-f Dockerfile .

