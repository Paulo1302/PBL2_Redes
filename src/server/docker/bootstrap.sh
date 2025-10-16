#!/bin/bash

# Script de bootstrap simplificado que delega a orquestração ao Docker Compose.
echo " Card Game Distributed Servers Bootstrap"
echo "=========================================="

set -e # Termina o script imediatamente se um comando falhar.

# Configurar cores para output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

# Caminho corrigido para o docker-compose.yml
COMPOSE_FILE="docker/docker-compose.yml"

# Parar e limpar containers existentes
log_info "A parar e a remover quaisquer serviços existentes..."
docker-compose -f ${COMPOSE_FILE} down --volumes

# Construir as imagens Docker
log_info "A construir as imagens Docker..."
docker-compose -f ${COMPOSE_FILE} build

# Iniciar todo o cluster
log_info "A iniciar o cluster... O Docker Compose irá gerir a ordem de arranque."
docker-compose -f ${COMPOSE_FILE} up -d

log_success "Comando 'up' concluído. A aguardar que os serviços estabilizem..."

# Aguarda um pouco e depois verifica o estado final
sleep 15 # Aumentar o sleep para aguardar o Raft e os healthchecks
echo ""
echo " A verificar o estado final dos servidores..."
echo "=========================================="
# O servidor 1 é o líder de bootstrap
log_info "Servidor 1 (Líder potencial):"
curl -s http://localhost:8081/status # A porta 8081 é a porta HOST do S1
log_info "Servidor 2 (Seguidor potencial):"
curl -s http://localhost:8082/status
log_info "Servidor 3 (Seguidor potencial):"
curl -s http://localhost:8083/status

echo ""
log_success "Configuração do cluster concluída!"
echo "=============================="
echo " Endpoints Disponíveis:"
echo "  • Monitor NATS: http://localhost:8222"
echo "  • Servidor 1:   http://localhost:8081"
echo "  • Servidor 2:   http://localhost:8082"
echo "  • Servidor 3:   http://localhost:8083"