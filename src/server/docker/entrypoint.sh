#!/bin/sh

# Este script é executado pelo ENTRYPOINT do Dockerfile.
# Ele descobre o IP do host e o injeta como a flag --raft-addr.

set -e

# 1. Encontra a --raft-port nos argumentos, ou usa o padrão 7000.
# (Baseado no default do main.go)
RAFT_PORT=7000
for arg in "$@"; do
    if echo "$arg" | grep -q -E -- "-raft-port="; then
        RAFT_PORT=$(echo "$arg" | cut -d'=' -f2)
    fi
done

# 2. Descobre o IP do host (funciona com --network=host)
# Tenta encontrar a rota principal para a internet (ex: 1.1.1.1)
# e extrai o IP de origem (src)
HOST_IP=$(ip route get 1.1.1.1 | awk -F"src " 'NR==1{print $2}' | awk '{print $1}')

if [ -z "$HOST_IP" ]; then
    echo "Erro fatal: Não foi possível descobrir o IP do host." >&2
    echo "Certifique-se de que o contêiner está rodando com --network=host" >&2
    exit 1
fi

# 3. Constrói o endereço
RAFT_ADDR="$HOST_IP:$RAFT_PORT"

# 4. Executa o comando principal, passando todos os argumentos originais ("$@")
#    e anexando o --raft-addr que acabamos de descobrir.
echo "[Entrypoint] Iniciando servidor. ID: $1. Endereço Raft descoberto: $RAFT_ADDR"
exec ./server_app --raft-addr "$RAFT_ADDR" "$@"