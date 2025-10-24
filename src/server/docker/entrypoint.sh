#!/bin/sh
set -e

# 1. Salva os argumentos originais (ex: --id 2, --port 8081, etc.)
ORIG_ARGS="$@"

# 2. Analisa os argumentos para encontrar a porta Raft
RAFT_PORT=7000 # Padrão do main.go

# Este loop processa os argumentos um por um
while [ "$#" -gt 0 ]; do
    case "$1" in
        # Lida com: --raft-port 7001
        --raft-port)
            if [ -n "$2" ]; then
                RAFT_PORT="$2"
            fi
            shift # Pula o valor
            ;;
        # Lida com: --raft-port=7001
        --raft-port=*)
            RAFT_PORT="${1#*=}"
            shift
            ;;
        *)
            shift # Ignora outras flags
            ;;
    esac
done

# 3. Descobre o IP do host
HOST_IP=$(ip route get 1.1.1.1 | awk -F"src " 'NR==1{print $2}' | awk '{print $1}')

if [ -z "$HOST_IP" ]; then
    echo "Erro fatal: Não foi possível descobrir o IP do host." >&2
    exit 1
fi

# 4. Constrói o endereço
RAFT_ADDR="$HOST_IP:$RAFT_PORT"

echo "[Entrypoint] Endereço Raft descoberto: $RAFT_ADDR"

# 5. Executa o comando principal, passando o raft-addr PRIMEIRO
#    e, em seguida, todos os argumentos originais.
exec ./server_app --raft-addr "$RAFT_ADDR" $ORIG_ARGS