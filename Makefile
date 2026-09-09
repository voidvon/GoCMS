.PHONY: backend frontend embedded-assets build generate normalize-images test release release-dry-run
backend:
	cd backend && go run ./cmd/site -addr 127.0.0.1:18080
frontend:
	cd frontend && npm run dev -- --host 127.0.0.1
embedded-assets:
	node scripts/sync-embedded.mjs
build:
	cd frontend && npm ci && npm run build
	node scripts/sync-embedded.mjs
	cd backend && go build -o ../bin/site ./cmd/site
generate:
	cd backend && go run ./cmd/generate
normalize-images:
	cd backend && go run ./cmd/normalize-images
test:
	cd frontend && npm run build
	node scripts/sync-embedded.mjs
	cd backend && go test ./... && go vet ./...
release:
	node scripts/release.mjs
release-dry-run:
	node scripts/release.mjs --dry-run
