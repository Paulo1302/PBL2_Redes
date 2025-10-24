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

Passo 1: Construir a Imagem (Uma Vez)

Primeiro, construa a sua imagem Docker. Esta única imagem será usada para o líder e para todos os followers.

Execute este comando a partir do seu diretório server/:
Bash

# (Use --no-cache se você alterou o main.go ou entrypoint.sh)
docker build -f docker/server.dockerfile -t meu-servidor-raft .

Passo 2: Iniciar o Servidor Líder

O líder usará o CMD padrão do seu server.dockerfile.

Bash

# (Opcional) Limpe qualquer líder antigo:
docker rm raft-leader

# 1. Inicie o Líder
docker run -d --name raft-leader --network=host meu-servidor-raft

    --network=host: É crucial. Permite que o entrypoint.sh descubra o IP real da sua máquina (ex: 192.168.0.106).

    O entrypoint.sh detetará a porta padrão (7000) e o IP do host, executando o server_app com as flags --raft-addr "IP_DO_LIDER:7000" --id "1" --bootstrap "true" ....

Passo 3: Descobrir o IP do Líder

Você precisará do IP real do líder para que os followers possam encontrá-lo. Na máquina do líder, execute:
Bash

ip a | grep 'inet '

(Procure o seu IP de rede principal, por exemplo: 192.168.0.106. Vamos chamar isto de IP_DO_LIDER.)

Passo 4: Iniciar os Followers

Agora, inicie os seus dois followers. Você deve substituir IP_DO_LIDER pelo IP que encontrou no Passo 3.

Importante: Se estiver a iniciar os followers na mesma máquina que o líder (para testes), você deve usar portas diferentes para cada um. Se eles estiverem em máquinas diferentes, pode usar as mesmas portas (8080/7000).

Cenário A: Followers na MESMA Máquina (Modo de Teste)

Bash

# (Opcional) Limpe followers antigos:
docker rm raft-follower1 raft-follower2

# 1. Inicie o Follower 1 (em portas 8081/7001)
docker run -d --name raft-follower1 --network=host meu-servidor-raft \
  --id "follower1" \
  --port 8081 \
  --raft-port 7001 \
  --peers "IP_DO_LIDER"

# 2. Inicie o Follower 2 (em portas 8082/7002)
docker run -d --name raft-follower2 --network=host meu-servidor-raft \
  --id "follower2" \
  --port 8082 \
  --raft-port 7002 \
  --peers "IP_DO_LIDER"

    O entrypoint.sh detetará as flags --raft-port 7001 e --raft-port 7002, anunciando os endereços corretos ao líder.

Cenário B: Followers em Máquinas DIFERENTES (Modo de Produção)

(Execute isto nas suas outras máquinas. Certifique-se de que a imagem meu-servidor-raft existe nelas.)
Bash

# (Opcional) Limpe followers antigos:
docker rm raft-follower1 raft-follower2

# 1. Inicie o Follower 1 (na Máquina 2)
docker run -d --name raft-follower1 --network=host meu-servidor-raft \
  --id "follower1" \
  --port 8080 \
  --raft-port 7000 \
  --peers "IP_DO_LIDER"

# 2. Inicie o Follower 2 (na Máquina 3)
docker run -d --name raft-follower2 --network=host meu-servidor-raft \
  --id "follower2" \
  --port 8080 \
  --raft-port 7000 \
  --peers "IP_DO_LIDER"

    O entrypoint.sh em cada máquina descobrirá o IP dessa máquina e anunciá-lo-á corretamente ao líder.
