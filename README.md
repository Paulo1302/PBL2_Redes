# PBL2_Redes
Código do segundo problema do MI de Concorrência e Conectividade TEC502



Referências:

[Utilização de PUB-SUB](https://nats.io/)

[Framework](https://gin-gonic.com/)

[Lib para implementação da comunicação entre servidores](https://hazelcast.com/developers/clients/go/)



DETERMINAMOS:

portas do servidores: já vão estar hardcoded
jogo: 3 cartas por usuário (NUMERO INTEIRO), qm tiver a maior ganha

jogadores só vão conectar a um servidor predeterminado 

REQUISICOES (client-side):
criar conta
logar

abrir pacote
ver cartas
trocar cartas
entrar em partida (fila compartilhada)

jogar carta (botar timer, se não jogar sai da partida)
desistencia

Guia de Compilação e Execução (Docker)

Este projeto utiliza Docker com um script de entrypoint para descobrir automaticamente o IP do host, permitindo que os nós do cluster se conectem em diferentes máquinas sem a necessidade de alterar o código-fonte.
Pré-requisitos

Certifique-se de que os seguintes arquivos existem no seu projeto:

    server/main.go (O seu código-fonte principal)

    server/docker/server.dockerfile (O Dockerfile que inclui o entrypoint.sh)

    server/docker/entrypoint.sh (O script que descobre o IP do host e injeta a flag --raft-addr)

1. Compilação da Imagem

Todos os nós (Líderes e Followers) usam a mesma imagem Docker. Você só precisa construir a imagem uma vez.

Execute no diretório server/ (onde está o seu main.go):

bash
docker build --no-cache -f docker/server.dockerfile -t meu-servidor-raft .

2. Limpar e Re-executar o Líder

Antes de iniciar um novo container do líder, pare e remova o container antigo para evitar conflitos:

bash
docker stop raft-leader && docker rm raft-leader

Inicie o container do líder com:

bash
docker run -d --name raft-leader \
  -p 7000:7000 -p 8080:8080 \
  -v raft-leader-data:/app/data \
  meu-servidor-raft \
  /app/entrypoint.sh --id leader --port 8080 --raft-port 7000 --bootstrap true

3. Iniciar Followers

Para iniciar followers, substitua --id e as portas para evitar conflitos:

bash
docker run -d --name raft-follower1 \
  -p 7001:7000 -p 8081:8080 \
  -v raft-follower1-data:/app/data \
  meu-servidor-raft \
  /app/entrypoint.sh --id follower1 --port 8080 --raft-port 7000 --peers raft-leader:7000

4. Verificar o status do cluster

Para verificar o status do líder e membros conectados:

bash
curl http://localhost:8080/status

A saída mostrará os nós atuantes no cluster com seus respectivos endereços.

Este guia garante que cada nó usa seu diretório de persistência próprio, configura o entrypoint.sh para detectar IPs automaticamente e permite escalabilidade em múltiplas máquinas sem alteração de código.

Quer que ajude a adicionar exemplos completos de configuração para followers diferentes, scripts auxiliares de monitoramento, ou documentação para troubleshooting?