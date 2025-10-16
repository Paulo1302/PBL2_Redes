#!/bin/sh

# Script para aguardar o cluster e rodar os testes
# Note: Este script é executado DENTRO do contêiner de teste.

# O Node 1 (Bootstrap) leva tempo para iniciar o Raft e se declarar Líder.
echo "Aguardando 15 segundos para o cluster Raft estabilizar e o Líder ser eleito..."
sleep 15

# Navega para a pasta de testes (conforme definido no Dockerfile)
# Assumimos que o código do Go foi copiado para a pasta /app
cd /app/src/server/tests 

# Executa os testes de integração
echo "Iniciando testes de integração..."
# O go test compila e executa os testes
go test -v 

# Captura o status de saída dos testes
TEST_STATUS=$?

echo "Fim dos testes. Status de saída: $TEST_STATUS"

# O Docker Compose usará este código de saída para determinar o sucesso/falha
exit $TEST_STATUS