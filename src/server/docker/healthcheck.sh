#!/bin/bash

# healthcheck.sh
# Verifica a saúde do nó Raft/API

# Verifica se o endpoint da API está rodando
STATUS_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/status)

if [ "$STATUS_CODE" -ne 200 ]; then
    echo "API não está respondendo (Status: $STATUS_CODE)"
    exit 1
fi

# Opcional: Adicionar checagem de estado Raft (depende do output do /status)
# Por simplicidade, a checagem de /status é suficiente para a saúde básica.

exit 0