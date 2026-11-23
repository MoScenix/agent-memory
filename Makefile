.DEFAULT_GOAL := help

svc ?= agent_memory
MODULE := github.com/MoScenix/agent-memory
CWGO ?= cwgo
PROTOC ?= protoc
DOCKER_COMPOSE ?= $(shell docker compose version >/dev/null 2>&1 && echo 'docker compose' || echo 'docker-compose')

.PHONY: help gen gen-server run run-outbox migrate test tidy lint vet env-start env-stop env-status
help:
	@echo "make gen svc=agent_memory   Regenerate protobuf models and Hertz routes"
	@echo "make run svc=agent_memory   Run the HTTP service (GO_ENV=dev by default)"
	@echo "make run-outbox             Run the Outbox worker"
	@echo "make migrate                Initialize the MySQL tables and foreign keys"
	@echo "make test                  Run service tests"
	@echo "make tidy                  Tidy Go dependencies"
	@echo "make lint                  Format Go source"
	@echo "make vet                   Run go vet"
	@echo "make env-start             Start MySQL and Milvus and wait for health checks"
	@echo "make env-stop              Stop middleware and keep its volumes"
	@echo "make env-status            Show middleware container status"

gen: gen-server

gen-server:
	$(PROTOC) -I "$(CURDIR)/idl" --descriptor_set_out=/dev/null "$(CURDIR)/idl/$(svc).proto"
	cd "app/$(svc)" && $(CWGO) server --type HTTP --server_name "$(subst _,-,$(svc))" --module "$(MODULE)/app/$(svc)" --idl "$(CURDIR)/idl/$(svc).proto" --pass "--exclude_file biz/handler/$(svc)/$(svc)_service_test.go"
	go -C "app/$(svc)" mod edit -dropreplace=github.com/apache/thrift

run:
	go -C "app/$(svc)" run .

run-outbox:
	go -C app/agent_memory run ./cmd/outbox-worker

migrate:
	go -C app/agent_memory run ./cmd/migrate

test:
	go -C "app/$(svc)" test ./...

tidy:
	go -C "app/$(svc)" mod tidy

lint:
	gofmt -l -w app

vet:
	go -C "app/$(svc)" vet ./...

env-start:
	$(DOCKER_COMPOSE) up -d --wait --wait-timeout 180

env-stop:
	$(DOCKER_COMPOSE) down

env-status:
	$(DOCKER_COMPOSE) ps
