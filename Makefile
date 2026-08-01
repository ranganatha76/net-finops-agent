.PHONY: build test lint run-scan run-remediate run-serve compose-up compose-down clean

BINARY := bin/net-finops-agent
CONFIG ?= configs/config.yaml

build:
	@mkdir -p bin
	go build -o $(BINARY) ./cmd/net-finops-agent

test:
	go test ./... -count=1

lint:
	go vet ./...

run-scan: build
	./$(BINARY) scan --config $(CONFIG) --mode demo --format table

run-estimate: build
	./$(BINARY) estimate --config $(CONFIG) --mode demo

run-remediate: build
	./$(BINARY) remediate --config $(CONFIG) --mode demo --format table

# Microservice: HTTP API + Prometheus /metrics (Grafana scrapes via compose stack)
run-serve: build
	./$(BINARY) serve --config $(CONFIG) --mode demo --addr :8080 --interval 1m

compose-up:
	docker compose -f deploy/docker-compose.yml up --build -d

compose-down:
	docker compose -f deploy/docker-compose.yml down

clean:
	rm -rf bin reports
