.PHONY: test build migrate
test:
	cargo fmt --manifest-path cli/Cargo.toml --check
	cargo test --manifest-path cli/Cargo.toml
	cd server && go test ./...
	cd server && go vet ./...
	cd web && npm run build
migrate:
	cd server && go run ./cmd/migrate
build:
	cargo build --release --manifest-path cli/Cargo.toml
	cd server && go build ./cmd/app
	cd web && npm run build
