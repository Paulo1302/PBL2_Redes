#!/bin/bash

# Script para inicialização manual de servidores

echo "Card Game Distributed Servers Bootstrap"

# Parar containers existentes
docker-compose -f docker/docker-compose.yml down

# Construir imagens
docker-compose -f docker/docker-compose.yml build

# Iniciar broker NATS primeiro
echo "Starting NATS broker..."
docker-compose -f docker/docker-compose.yml up -d nats-broker
sleep 5

# Iniciar servidor bootstrap
echo "Starting bootstrap server..."
docker-compose -f docker/docker-compose.yml up -d game-server-1
sleep 10

# Iniciar servidores adicionais
echo "Starting additional servers..."
docker-compose -f docker/docker-compose.yml up -d game-server-2 game-server-3

# Aguardar inicialização
sleep 10

# Verificar status
echo "Checking server status..."
curl -s http://localhost:8081/health | jq .
curl -s http://localhost:8082/health | jq .
curl -s http://localhost:8083/health | jq .

echo "Cluster setup complete!"
echo "Server 1: http://localhost:8081"
echo "Server 2: http://localhost:8082"
echo "Server 3: http://localhost:8083"
