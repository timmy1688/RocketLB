.PHONY: test run web build tidy db start stop restart status

test:
	go test ./...

db:
	docker compose up -d --wait postgres

start:
	./scripts/service.sh start

stop:
	./scripts/service.sh stop

restart:
	./scripts/service.sh restart

status:
	./scripts/service.sh status

run: start

web:
	npm --prefix web install
	npm --prefix web run dev

tidy:
	go mod tidy

build:
	npm --prefix web install
	npm --prefix web run build
	go build -o bin/rocketlb ./cmd/rocketlb
