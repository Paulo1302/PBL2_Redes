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

# Parar e limpar containers existentes
log_info "A parar e a remover quaisquer serviços existentes..."
docker-compose -f docker/docker-compose.yml down --volumes

# Construir as imagens Docker
log_info "A construir as imagens Docker..."
docker-compose -f docker/docker-compose.yml build

# Iniciar todo o cluster
# O Docker Compose irá respeitar o 'depends_on' e 'condition: service_healthy'
# para iniciar os serviços na ordem correta.
log_info "A iniciar o cluster... O Docker Compose irá gerir a ordem de arranque."
docker-compose -f docker/docker-compose.yml up -d

log_success "Comando 'up' concluído. A aguardar que os serviços estabilizem..."

# Aguarda um pouco e depois verifica o estado final
sleep 5
echo ""
echo " A verificar o estado final dos servidores..."
echo "=========================================="
curl -s http://localhost:8081/health | jq .
curl -s http://localhost:8082/health | jq .
curl -s http://localhost:8083/health | jq .

echo ""
echo " Configuração do cluster concluída!"
echo "=============================="
echo " Endpoints Disponíveis:"
echo "  • Monitor NATS: http://localhost:8222"
echo "  • Servidor 1:   http://localhost:8081"
echo "  • Servidor 2:   http://localhost:8082"
echo "  • Servidor 3:   http://localhost:8083"