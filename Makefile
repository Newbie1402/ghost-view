.PHONY: fmt vet test build run acceptance live-acceptance integration smoke browser check

fmt:
	gofmt -w $$(find cmd internal -name '*.go')
vet:
	go vet ./...
test:
	mkdir -p artifacts
	go test -race -coverpkg=./internal/... -coverprofile=artifacts/coverage.out ./...
	go tool cover -func=artifacts/coverage.out
build:
	mkdir -p bin
	go build -trimpath -o bin/ghostview ./cmd/server
run:
	go run ./cmd/server
acceptance:
	bash scripts/acceptance.sh
live-acceptance:
	bash scripts/live-acceptance.sh
integration:
	bash scripts/integration.sh
smoke:
	python3 tests/frontend_smoke.py
browser:
	node tests/browser-smoke.cjs
check: vet test build acceptance smoke
