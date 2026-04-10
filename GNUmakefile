default: build

build:
	go build -v ./...

install: build
	go install -v ./...

lint:
	golangci-lint run

fmt:
	gofmt -s -w .

test:
	go test -v -cover -timeout=120s -parallel=4 ./...

testacc:
	TF_ACC=1 go test -v -cover -timeout 120m ./...

generate:
	go generate ./...

.PHONY: build install lint fmt test testacc generate
