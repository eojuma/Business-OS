.PHONY: up down build logs backend-run backend-test frontend-dev migrate-up migrate-down seed

up:
	docker compose up -d

down:
	docker compose down

build:
	docker compose build

logs:
	docker compose logs -f

backend-run:
	cd backend && go run ./cmd/api

backend-test:
	cd backend && go test ./...

backend-tidy:
	cd backend && go mod tidy

frontend-dev:
	cd frontend && npm run dev

migrate-up:
	cd backend && go run ./cmd/api migrate up

migrate-down:
	cd backend && go run ./cmd/api migrate down

seed:
	cd backend && go run ./cmd/api seed