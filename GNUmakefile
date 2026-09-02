default: build

build:
	go build -o terraform-provider-fivenines

install: build
	mkdir -p ~/.terraform.d/plugins/registry.terraform.io/Five-Nines-io/fivenines/0.1.0/$$(go env GOOS)_$$(go env GOARCH)
	cp terraform-provider-fivenines ~/.terraform.d/plugins/registry.terraform.io/Five-Nines-io/fivenines/0.1.0/$$(go env GOOS)_$$(go env GOARCH)/

test:
	go test ./... -v

testacc:
	TF_ACC=1 go test ./internal/provider/ ./internal/client/ -v -timeout 120m

fmt:
	go fmt ./...

docs:
	tfplugindocs generate --provider-name terraform-provider-fivenines

.PHONY: default build install test testacc fmt docs
