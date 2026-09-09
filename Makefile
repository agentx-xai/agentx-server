.PHONY: test build migrate production-preflight
test:
	go test ./...
	go vet ./...
migrate:
	go run ./cmd/migrate
build:
	go build ./cmd/app
	go build ./cmd/migrate
	go build ./cmd/production-preflight
production-preflight:
	go run ./cmd/production-preflight -kustomize-dir "$${KUSTOMIZE_DIR:-deploy/kubernetes}"
