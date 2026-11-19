.PHONY: dev-db migrate seed api worker test db-test check openapi
# Export .env explicitly in your shell; Make does not silently load secrets.
dev-db:
	./scripts/dev-db.sh
migrate:
	go run ./cmd/manage migrate
seed:
	go run ./cmd/manage seed
api:
	go run .
worker:
	go run ./cmd/worker
test:
	go test -count=1 ./...
db-test:
	./db/tests/run_all.sh spartial_test
check:
	test -z "$$(gofmt -l main.go cmd internal api tests)"
	go vet ./...
	go test -race -count=1 ./...
	go build . ./cmd/...
	go mod verify
openapi:
	python3 scripts/generate-openapi.py
