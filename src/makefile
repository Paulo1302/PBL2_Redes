cd ~/Documentos/PBL2_Redes/src

# Fazer backup do makefile atual
cp makefile makefile.backup

# Criar novo makefile (copie o conteúdo do arquivo makefile-novo.txt[178])
cat > makefile << 'EOF'
.PHONY: broker build run test clean help dev-client build-client rmAll

# Comandos originais preservados
broker:
	@sudo docker run -p 4222:4222 nats

rmAll:
	@sudo docker system prune -a --volumes

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
	@cd server && go build -o ../game-server .

# Build client
build-client:
	@echo "Building client..."
	@cd client && go build -o ../game-client .

# Run server locally
run: build
	@echo "Starting server..."
	@SERVER_ID=local-server \
	HTTP_PORT=8080 \
	IS_BOOTSTRAP=true \
	./game-server

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
EOF
