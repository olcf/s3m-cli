BUILDDIR ?= binaries/
GOCMD ?= go

.PHONY: test
test:
	GOFLAGS="-mod=vendor" go test -coverprofile=${COVERAGE_FILE:-cover.out} ./...

.PHONY: build
build:
	GOFLAGS="-mod=vendor" $(GOCMD) build -o $(BUILDDIR)s3m ./cmd/main

# Protobufs
S3M_APIS_REF ?= v0.3.4
PROTOBUILD_FLAGS := $(if $(wildcard .venv),--venv .venv) $(if $(wildcard .go),--go .go)

.PHONY: install-protobuild
install-protobuild:
	pip3 install https://codeload.github.com/olcf/s3m-protobuild/zip/refs/heads/main

.PHONY: setup-protos
setup-protos:
	s3m-protobuild setup python go --venv .venv --go .go

.PHONY: protos
protos:
	rm -rf internal/pkg/s3m-apis
	s3m-protobuild build $(PROTOBUILD_FLAGS) \
		--source git+https://github.com/olcf/s3m-apis.git@$(S3M_APIS_REF) \
		--out internal/pkg/s3m-apis \
		--descriptor-out desc.bin \
		--descriptor-imports \
		common:go \
		slurm/v0042:go \
		slurm/v0043:go \
		status/v1alpha:go \
		storage/v1alpha:go \
		streaming/v1alpha:go \
		tms/v1:go
	mv ./internal/pkg/s3m-apis/desc.bin ./internal/embedded/s3m-apis.desc.bin

.PHONY: clean
clean:
	${GOCMD} clean
	rm -rf $(BUILDDIR) bom/
	rm -rf internal/pkg/s3m-apis .venv .go
	rm ./internal/embedded/s3m-apis.desc.bin
