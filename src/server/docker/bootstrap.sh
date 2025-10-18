#!/bin/bash

# Script de gerenciamento para cluster Raft escalável no Docker Compose.

set -e

GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'
COMPOSE_FILE="docker-compose.yml"

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_error() { echo -e "\033[0;31m[ERROR]${NC} $1"; exit 1; }

# Função auxiliar para executar o compose
compose_exec() {
    docker compose -f ${COMPOSE_FILE} "$@"
}

# CORREÇÃO: Função para forçar o join do follower (executando dentro do container)
# Nota: Esta função assume que a lógica de join do main.go é acionada 
# pelo comando `go run main.go ...` e não por uma requisição API externa.
# Se a lógica de join for via API externa (curl), seria diferente.
# Pelo código Go, o join é acionado *se* --peers for definido.
force_join() {
    local service_name=$1
    log_info "A forçar o join do nó ${service_name} com o Líder..."

    # Usa 'docker exec' para rodar um comando dentro do container do follower.
    # O nó follower precisa saber:
    # 1. Seu próprio ID (ex: node2)
    # 2. Sua própria porta Raft (ex: 7001)
    # 3. O endereço Raft do Líder (--peers game-leader:7000)
    # 4. A porta HTTP do Líder (8080)
    
    # Executa o main.go dentro do container com as flags corretas para join
    docker compose exec -d ${service_name} /usr/local/bin/server \
        --id=${service_name} \
        --port=8080 \
        --raft-port=7000 \
        --peers=game-leader:7000
    
    # Nota de melhoria: A porta raft deve ser dinâmica (7000, 7001, etc.)
    # Se a porta Raft é fixa em 7000, o Raft não funciona. 
    # Assumimos que o docker-compose.yml mapeia 7000 para a porta correta.
}


# ====================================================
# FLUXO DE EXECUÇÃO
# ====================================================

case "$1" in
    # ... (build e start-leader permanecem inalterados)

    scale)
        if [ "$#" -ne 2 ]; then
            log_error "Uso: $0 scale <NÚMERO_DE_FOLLOWERS>"
        fi
        NUM_FOLLOWERS=$2
        
        log_info "A escalonar para ${NUM_FOLLOWERS} servidores followers..."
        
        # 1. Escala o serviço game-follower
        # Nota: O Docker Compose gerencia os nomes como game-follower-1, game-follower-2, etc.
        compose_exec up -d --scale game-follower=${NUM_FOLLOWERS}
        log_success "Escalonamento concluído. ${NUM_FOLLOWERS} containers de followers ativos."

        # 2. CORREÇÃO: Aciona o join para cada follower
        # Precisamos de um pequeno delay para garantir que o container suba
        sleep 5 
        
        for i in $(seq 1 ${NUM_FOLLOWERS}); do
            FOLLOWER_SERVICE="game-follower-${i}"
            
            # Executa a lógica de join do binário Go dentro do container
            # Esta é a abordagem mais direta: rodar o binário Go com as flags de join.
            log_info "Disparando o comando de Join para o container: ${FOLLOWER_SERVICE}..."
            
            # Executa o binário do servidor (assumindo que o binário se chama 'server')
            # O ID é o nome do container (game-follower-1), e o peer é o Leader.
            docker compose exec -d ${FOLLOWER_SERVICE} /usr/local/bin/server \
                --id=${FOLLOWER_SERVICE} \
                --port=8080 \
                --raft-port=7000 \
                --peers=game-leader:7000 # O Leader é sempre o peer primário
            
            sleep 1 # Pequeno delay entre joins
        done
        log_success "Processo de Join acionado para todos os ${NUM_FOLLOWERS} followers."
        ;;
        
    # ... (stop, logs e default permanecem inalterados)
    
    *)
        echo "Uso: $0 {build|start-leader|scale <N>|stop|logs}"
        exit 1
esac