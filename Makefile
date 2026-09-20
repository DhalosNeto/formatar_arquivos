# Formatador Acadêmico — atalhos de desenvolvimento.
SHELL := /bin/bash
COMPOSE_ARQUIVO := deploy/docker-compose.yml

# Detecta o runtime de containers disponível na máquina.
COMPOSE := $(shell \
	if docker compose version >/dev/null 2>&1; then echo "docker compose"; \
	elif command -v docker-compose >/dev/null 2>&1; then echo "docker-compose"; \
	elif podman compose version >/dev/null 2>&1; then echo "podman compose"; \
	elif command -v podman-compose >/dev/null 2>&1; then echo "podman-compose"; \
	fi)

.PHONY: ajuda up down logs seed migrar build test test-integration test-e2e lint fmt cobertura front-install front-dev verificar-compose

ajuda: ## Lista os alvos disponíveis
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

verificar-compose:
	@if [ -z "$(COMPOSE)" ]; then \
		echo "Nenhum runtime de compose encontrado. Instale o Docker Compose ou o podman-compose."; \
		exit 1; \
	fi

up: verificar-compose ## Sobe todos os serviços
	$(COMPOSE) -f $(COMPOSE_ARQUIVO) up -d --build

down: verificar-compose ## Derruba os serviços
	$(COMPOSE) -f $(COMPOSE_ARQUIVO) down

logs: verificar-compose ## Acompanha os logs
	$(COMPOSE) -f $(COMPOSE_ARQUIVO) logs -f

migrar: ## Aplica as migrations no banco
	cd backend && go run github.com/pressly/goose/v3/cmd/goose@latest -dir migrations postgres "$${POSTGRES_DSN}" up

seed: ## Carrega os rulesets YAML no banco
	cd backend && go run ./cmd/rulesetctl semear --dir rulesets

build: ## Compila os binários do backend
	cd backend && go build -o bin/ ./cmd/...

test: ## Roda os testes unitários com corrida e cobertura
	cd backend && go test ./... -race -coverprofile=coverage.out -covermode=atomic
	cd backend && go tool cover -func=coverage.out | tail -1

test-integration: ## Roda os testes de integração (requer containers)
	cd backend && go test -tags=integration ./... -race

test-e2e: ## Roda os testes ponta a ponta do front
	cd frontend && npm run test:e2e

cobertura: test ## Abre o relatório de cobertura no navegador
	cd backend && go tool cover -html=coverage.out

lint: ## Roda os linters do backend e do front
	cd backend && gofmt -l . && go vet ./...
	cd backend && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...
	cd frontend && npm run lint && npm run typecheck

fmt: ## Formata o código Go
	cd backend && gofmt -w .

front-install: ## Instala as dependências do front
	cd frontend && npm install

front-dev: ## Sobe o front em modo desenvolvimento
	cd frontend && npm run dev
