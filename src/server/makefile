.PHONY: broker build run test clean help dev-client build-client rmAll

# Comandos originais preservados
broker:
	@docker run -p 4222:4222 nats

rmAll:
	@docker system prune -a --volumes

# Comandos corrigidos para estrutura atual
help:
	@echo "Available commands:"
	@echo "  broker        - Start NATS broker (original)"
	@echo "  rmAll         - Clean all Docker resources (original)"
	@echo "  build         - Build server"
	@echo "  run           - Run server locally"
	@echo "  test          - Test server endpoints"
	@echo "  dev-client    - Run development client"
	@echo "  build-client  - Build client"
	@echo "  clean         - Clean build artifacts"

# Build server (CORRIGIDO: vai para server/ não cardGame/server)
build:
	@echo "Building server..."
	@docker build -f docker/server.dockerfile -t meu-servidor-raft .

# Build client
build-client:
	@echo "Building client..."
	@cd client && go build -o ../game-client .

# Run server locally
run-leader: 
	@echo "Starting server..."
	@docker run -d --name raft-leader --network=host meu-servidor-raft

run-follower1: 
	@echo "Starting server..."
	@docker run -d --name raft-follower1 --network=host meu-servidor-raft --id "follower1" --port 8081 --raft-port 7001 --peers "192.168.0.21"

run-follower2: 
	@echo "Starting server..."
	@docker run -d --name raft-follower2 --network=host meu-servidor-raft --id "follower2" --port 8082 --raft-port 7002 --peers "192.168.0.21"

# Run development client
dev-client:
	@echo "Starting development client..."
	@cd client && go run . -server=0 -count=5 -interval=2s

# Test server endpoints
test:
	@echo "Testing server endpoints..."
	@echo "Testing /hello endpoint:"
	@curl -s http://localhost:8080/hello || echo "Server not running"
	@echo ""
	@echo "Testing /health endpoint:"
	@curl -s http://localhost:8080/health || echo "Health endpoint not available"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -f game-server game-client
	@cd server && go clean
	@cd client && go clean

# Quick test - build and run for 10 seconds
quick-test: build
	@echo "Running quick test..."
	@timeout 10s bash -c 'SERVER_ID=test-server HTTP_PORT=8080 ./game-server' &
	@sleep 3
	@curl -s http://localhost:8080/hello || echo "Server test failed"
	@pkill -f game-server || true

# Check dependencies
deps:
	@echo "Checking dependencies..."
	@go mod tidy
