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

Inicializando o Servidor Líder no Docker
1. Construir a imagem do servidor

Execute no diretório raiz do projeto (src/server):

bash
docker build --no-cache -f docker/server.dockerfile -t meu-servidor-raft-lider .

2. Limpar e Re-executar o Líder

Antes de rodar novamente o líder, remova qualquer container existente com o mesmo nome:

bash
docker stop raft-leader && docker rm raft-leader

Em seguida, inicie um novo container limpo:

bash
docker run -d --name raft-leader \
  -p 7000:7000 -p 8080:8080 \
  -v raft-leader-data:/app/data \
  meu-servidor-raft-lider \
  /app/server_app --id leader --raft-port 7000 --port 8080 --bootstrap true

3. Verificar o status do líder

Depois que o container iniciar, confirme que o servidor está ativo:

bash
curl http://localhost:8080/status

Uma resposta válida indicará que o líder foi inicializado com sucesso.