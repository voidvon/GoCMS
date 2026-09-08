.PHONY: backend frontend build generate normalize-images test
backend:
	cd backend && go run ./cmd/site -addr 127.0.0.1:18080
frontend:
	cd frontend && npm run dev -- --host 127.0.0.1
build:
	cd frontend && npm ci && npm run build
	cd backend && go build -o ../bin/site ./cmd/site
generate:
	cd backend && go run ./cmd/generate
normalize-images:
	cd backend && go run ./cmd/normalize-images
test:
	cd backend && go test ./... && go vet ./...
	cd frontend && npm run build
