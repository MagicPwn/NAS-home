SHELL := /bin/bash
ROOT := $(abspath .)
GO ?= go

.PHONY: backend-test backend-build frontend-install frontend-typecheck frontend-build proxy-test test build preflight compose-config
backend-test:
	cd backend && $(GO) test ./...
backend-build:
	cd backend && CGO_ENABLED=0 $(GO) build -trimpath -o nas-home ./cmd/nas-home
frontend-install:
	cd frontend && npm install --no-audit --no-fund
frontend-typecheck:
	cd frontend && npm run typecheck
frontend-build:
	cd frontend && npm run build
proxy-test:
	cd deploy/socket-proxy && GO111MODULE=off $(GO) test .

test: backend-test proxy-test frontend-typecheck
build: backend-build frontend-build
preflight:
	./scripts/preflight.sh
compose-config:
	docker compose --env-file deploy/.env.example -f deploy/compose.yml config --quiet
