.PHONY: dev backend backend-once frontend embedded-assets build generate test release release-dry-run

AIR := $(shell command -v air 2>/dev/null || ( [ -x "$$(go env GOPATH)/bin/air" ] && echo "$$(go env GOPATH)/bin/air" ))

dev:
	@echo "正在启动开发模式（后端 Air 热重载 + 前端 Vite 开发服务）..."
	@trap 'kill 0' SIGINT SIGTERM EXIT; \
	$(MAKE) backend & \
	$(MAKE) frontend & \
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
	cd frontend && npm run dev -- --host 127.0.0.1
embedded-assets:
	node scripts/sync-embedded.mjs
build:
	cd frontend && npm ci && npm run build
	node scripts/sync-embedded.mjs
	cd backend && go build -o ../bin/site ./cmd/site
generate:
	cd backend && go run ./cmd/generate
test:
	cd frontend && npm run build
	node scripts/sync-embedded.mjs
	cd backend && go test ./... && go vet ./...
release:
	node scripts/release.mjs
release-dry-run:
	node scripts/release.mjs --dry-run
