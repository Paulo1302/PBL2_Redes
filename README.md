# 🃏 Jogo de Cartas Multiplayer Distribuído com Raft e NATS

Este projeto implementa um **servidor de jogo de cartas multiplayer distribuído**, **tolerante a falhas** e de **alta disponibilidade**, escrito em **Go**.

O sistema é construído sobre uma **arquitetura híbrida** que combina:

* **Raft** (para consenso e consistência de estado),
* **API REST interna** (para comunicação entre servidores),
* **Broker de mensageria NATS** (para comunicação com o cliente).

---

## 🧱 Visão Geral da Arquitetura

A arquitetura do servidor é composta por **três camadas de comunicação** distintas que trabalham em conjunto:

### ⚙️ Consenso (Raft)

O **"coração"** do sistema.
Todos os nós do servidor formam um cluster Raft.
Todo o estado crítico do jogo — **listas de jogadores, inventários de cartas, estoque de pacotes, filas de pareamento** — é armazenado em uma **Máquina de Estado Replicada (FSM)** (`store.go`) e é 100% consistente em todos os nós.
Apenas o **nó Líder** do Raft pode fazer alterações no estado.

---

### 💬 Comunicação Cliente-Servidor (NATS Pub/Sub)

Os **clientes (`client/main.go`)** são “burros”:
Eles **não sabem** qual servidor é o líder ou quantos servidores existem.
Eles se comunicam **exclusivamente com o broker NATS**.

Exemplo:

* Para realizar uma ação (ex: *Abrir Pacote*), o cliente publica uma mensagem no tópico `topic.openPack` e aguarda a resposta (`pubsub.RequestOpenPack`).

---

### 🌐 Comunicação Servidor-Servidor (API REST)

Esta é a **“cola”** que liga o NATS ao Raft.

* Quando um cliente envia uma mensagem NATS, **qualquer nó** do servidor pode recebê-la.
* Se o nó que a recebe for o **Líder**, ele processa e aplica a mudança no Raft.
* Se for um **Seguidor**, ele usa `forwardToLeaderViaREST` (`server-api.go`) para encaminhar a solicitação via **HTTP** para o Líder.
  O Líder processa e devolve a resposta pelo mesmo caminho.

➡️ Este modelo garante:

* **Alta disponibilidade:** clientes podem se conectar a qualquer nó.
* **Tolerância a falhas:** o Raft mantém o estado consistente entre os nós.

---

## 🧩 Principais Tecnologias

| Tecnologia                  | Função                                          |
| --------------------------- | ----------------------------------------------- |
| **Go (Golang)**             | Linguagem principal para servidor e cliente     |
| **NATS**                    | Broker de mensageria (Pub/Sub cliente-servidor) |
| **Raft (hashicorp/raft)**   | Consenso e replicação de estado (FSM)           |
| **Gin**                     | Framework para API REST interna                 |
| **Docker & Docker Compose** | Containerização e orquestração                  |
| **Make**                    | Automação de build e execução                   |

---

## ⚙️ Pré-requisitos

* **Go** (v1.24 ou superior)
* **Docker** e **Docker Compose**
* **make**

---

## ⚠️ Atenção: Configuração Manual de IP Obrigatória

Este projeto requer **configuração manual de IPs** antes da execução.
O `makefile` e os arquivos `pubsub.go` contêm IPs de desenvolvimento **hardcoded**.

---

### 🧭 Passo 1: Configurar o IP do Broker NATS

1. Inicie o NATS:

   ```bash
   make broker
   ```
2. Descubra o IP da máquina que está rodando o broker (ex: `192.168.0.21`).
3. Atualize o IP nos seguintes arquivos:

#### 🔹 Servidor

Arquivo: `server/API/pubsub.go`

```go
url := "nats://10.200.54.149:" + strconv.Itoa(serverNumber+4222)
```

➡️ **Mude:** `10.200.54.149` → IP do broker.

#### 🔹 Cliente

Arquivo: `client/API/pubsub.go`

```go
url := "nats://10.200.54.149:" + strconv.Itoa(serverNumber+4222)
```

➡️ **Mude:** `10.200.54.149` → IP do broker.

---

### 🧭 Passo 2: Configurar o IP do Líder (Peers) no Makefile

O `makefile` usa `--network=host`, ou seja, os contêineres usam o IP da máquina host.
Descubra o IP da sua máquina (ex: `192.168.0.21`) e atualize no `makefile`.

Exemplo de trecho:

```makefile
run-follower1:
    @echo "Starting server..."
    @docker run -d --name raft-follower1 --network=host meu-servidor-raft \
      --id "follower1" --port 8081 --raft-port 7001 --peers "192.168.0.21"
      # ^^^^^^^^^^^^^ MUDE ESTE IP

run-follower2:
    @echo "Starting server..."
    @docker run -d --name raft-follower2 --network=host meu-servidor-raft \
      --id "follower2" --port 8082 --raft-port 7002 --peers "192.168.0.21"
      # ^^^^^^^^^^^^^ MUDE ESTE IP
```

---

## 🚀 Como Executar (Localmente)

Após a **configuração de IPs**, siga os passos abaixo (cada um em um terminal separado).

### 🧩 Terminal 1: Iniciar o Broker NATS

```bash
make broker
```

### 🧩 Terminal 2: Construir e Iniciar o Cluster Raft

```bash
# Construir imagem Docker
make build

# Iniciar o nó Líder
make run-leader

# Aguardar alguns segundos, depois iniciar os seguidores
make run-follower1
make run-follower2
```

### 🧩 Terminal 3: Executar o Cliente

```bash
cd client/
go run .
```

O cliente de console será iniciado e permitirá **criar conta, logar e jogar**.

---

## 🧪 Testes

### ▶️ Teste de Cliente

```bash
make dev-client
```

### 🩺 Teste de API (Saúde)

```bash
make test
```

*(Pode estar desatualizado)*

---

## 📁 Estrutura do Repositório (Simplificada)

```
/
├── client/
│   ├── API/
│   │   └── pubsub.go     # SDK do cliente para NATS
│   └── main.go           # Aplicação CLI do cliente
│
└── server/
    ├── API/
    │   ├── pubsub.go     # Lógica NATS (recebimento)
    │   ├── server-api.go # API REST interna (encaminhamento)
    │   ├── store.go      # FSM do Raft
    │   └── game_logic.go # Lógica de negócios
    │
    ├── docker/
    │   ├── server.dockerfile
    │   ├── entrypoint.sh
    │   └── ...
    │
    ├── main.go           # Inicializa Raft, NATS e REST
    └── makefile          # Build e execução
```

---

Deseja que eu adicione também **blocos de badges** (ex: Go version, Docker, License) e um **sumário automático** no início do README? Isso deixaria o arquivo mais profissional.
