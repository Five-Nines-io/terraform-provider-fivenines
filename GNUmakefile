default: build

build:
	go build -o terraform-provider-fivenines

install: build
	mkdir -p ~/.terraform.d/plugins/registry.terraform.io/Five-Nines-io/fivenines/0.1.0/$$(go env GOOS)_$$(go env GOARCH)
	cp terraform-provider-fivenines ~/.terraform.d/plugins/registry.terraform.io/Five-Nines-io/fivenines/0.1.0/$$(go env GOOS)_$$(go env GOARCH)/

test:
	go test ./... -v

testacc:
	TF_ACC=1 go test ./... -v

fmt:
	go fmt ./...

docs:
	tfplugindocs generate

.PHONY: default build install test testacc fmt docs
