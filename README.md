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
