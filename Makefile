.PHONY: dev backend backend-once frontend storefront embedded-assets build generate test release release-dry-run

AIR := $(shell command -v air 2>/dev/null || ( [ -x "$$(go env GOPATH)/bin/air" ] && echo "$$(go env GOPATH)/bin/air" ))

dev:
	@echo "正在启动开发模式（Go + 后台 Vite + 购物前台 Vite）..."
	@trap 'kill 0' SIGINT SIGTERM EXIT; \
	$(MAKE) backend & \
	$(MAKE) frontend & \
	$(MAKE) storefront & \
	wait

backend:
	@if [ -n "$(AIR)" ]; then \
		cd backend && "$(AIR)"; \
	else \
		echo "提示: 未检测到 air 工具，以普通模式启动。如需热重载请执行: go install github.com/air-verse/air@latest"; \
		cd backend && go run ./cmd/site -addr 127.0.0.1:18080; \
	fi

backend-once:
	cd backend && go run ./cmd/site -addr 127.0.0.1:18080
frontend:
	cd frontend && npm run dev -- --host 127.0.0.1 --port 1244
storefront:
	cd storefront && npm run dev
embedded-assets:
	node scripts/sync-embedded.mjs
build:
	cd frontend && npm ci && npm run build
	cd storefront && npm ci && npm run build
	node scripts/sync-embedded.mjs
	cd backend && go build -tags nodynamic -o ../bin/site ./cmd/site
generate:
	cd backend && go run ./cmd/generate
test:
	cd frontend && npm run build
	cd storefront && npm run build
	node scripts/sync-embedded.mjs
	cd backend && go test -tags nodynamic ./... && go vet -tags nodynamic ./...
release:
	node scripts/release.mjs
release-dry-run:
	node scripts/release.mjs --dry-run
