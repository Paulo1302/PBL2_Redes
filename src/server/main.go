package server

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	// 1. IMPORTAÇÃO CORRETA DOS PACOTES
	"server/API" // Ajuste este caminho para o seu módulo real
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/raft"
	"github.com/hashicorp/raft-boltdb"

)

// Estrutura de configuração que deve ser definida no seu módulo 'common'
type ServerConfig struct {
    ServerID  string
    HTTPPort  int
    RaftPort  int
    IsBootstrap bool
    KnownPeers []string
}

// NOTE: A função LoadConfig() deve ser implementada no seu módulo comum.
func LoadConfig() ServerConfig {
    // Simulação de LoadConfig, pois o arquivo não foi fornecido
    return ServerConfig{}
}

// A função main() é o ponto de entrada da aplicação.
func main() {
	// 1. Definição dos Flags (Argumentos de Linha de Comando)
	var (
		nodeID      string
		httpPort    int
		raftPort    int
		bootstrap   bool
		peersStr    string
	)

	flag.StringVar(&nodeID, "id", "", "ID exclusivo do nó Raft (ex: node1)")
	flag.IntVar(&httpPort, "port", 8080, "Porta HTTP para API REST")
	flag.IntVar(&raftPort, "raft-port", 7000, "Porta RPC Raft para comunicação interna")
	flag.BoolVar(&bootstrap, "bootstrap", false, "Definido para 'true' apenas no primeiro nó do cluster")
	flag.StringVar(&peersStr, "peers", "", "Lista de peers Raft (ex: '127.0.0.1:7000,127.0.0.1:7001')")
	flag.Parse()

	if nodeID == "" {
		log.Fatal("O ID do nó (--id) deve ser fornecido.")
	}

	// 2. Inicializa o FSM (Store) do pacote API
	store := API.NewStore()

	// 3. Configura o Raft
	raftLog, raftAddr, err := setupRaft(nodeID, raftPort, store, bootstrap)
	if err != nil {
		log.Fatalf("Erro ao configurar o Raft: %v", err)
	}

	// 4. Preenche os campos do Store (para contornar os erros de versão)
	store.RaftLog = raftLog
	store.NodeID = nodeID
	store.RaftAddr = raftAddr // Endereço de RPC do Raft
    
    log.Printf("Nó Raft (%s) inicializado em %s", store.NodeID, store.RaftAddr)

	// 5. Lógica de Join
	if !bootstrap && peersStr != "" {
		joinCluster(nodeID, raftPort, httpPort, peersStr)
	}
    
    // 6. Inicia o NATS (Comunicação Assíncrona)
    go startNATSCommunication()
    
	// 7. Inicia o Servidor HTTP (API REST)
	router := API.SetupRouter(store) // Chama a função do pacote API
	httpAddr := fmt.Sprintf(":%d", httpPort)
	log.Printf("Servidor HTTP (API) rodando em %s", httpAddr)

	httpServer := &http.Server{
		Addr:    httpAddr,
		Handler: router,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Falha ao iniciar o servidor HTTP: %v", err)
		}
	}()

	// 8. Tratamento de Shutdown (Parada Gratuita)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Encerrando o servidor...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Primeiro, fechar a comunicação HTTP
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Erro no shutdown do servidor HTTP: %v", err)
	}
    // TODO: Adicionar lógica para o shutdown do RaftLog aqui
    
	log.Println("Servidor interrompido com sucesso.")
}

// startNATSCommunication inicia a conexão NATS
func startNATSCommunication() {
    // Usamos o endereço NATS padrão (0+4222) conforme o pubsub.go
    nc, err := API.BrokerConnect(0)
	if err != nil {
		log.Printf("Falha ao conectar ao NATS broker: %v", err)
		return
	}
    defer nc.Close()
    
	// Configura o handler de resposta a ping do NATS
	if err := API.ReplyPing(nc); err != nil {
        log.Printf("Falha ao configurar a resposta a ping do NATS: %v", err)
        return
	}
    
    // Mantém a goroutine NATS em execução
    select {} 
}


// setupRaft configura e retorna a instância Raft, além do endereço de RPC.
func setupRaft(id string, port int, fsm *API.Store, bootstrap bool) (*raft.Raft, string, error) {
	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(id)
	config.Logger = hclog.New(&hclog.LoggerOptions{Name: id, Level: hclog.Info})

	// Setup Transport (RPC)
	raftAddr := fmt.Sprintf("127.0.0.1:%d", port)
	tcpAddr, err := net.ResolveTCPAddr("tcp", raftAddr)
	if err != nil {
		return nil, "", err
	}
	transport, err := raft.NewTCPTransport(raftAddr, tcpAddr, 10, 5*time.Second, os.Stderr)
	if err != nil {
		return nil, "", err
	}

	// Setup Log Store, Stable Store, Snapshot Store
	dataDir := filepath.Join("data", id)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, "", err
	}
	// TODO: Adicione importação e uso do raftboltdb
	// No momento, BoltDB não está importado no main.go, o que é um erro.
	boltDB, err := raftboltdb.NewBoltStore(filepath.Join(dataDir, "raft.db"))
	if err != nil {
		return nil, "", err
	}
    
	snapshotStore, err := raft.NewFileSnapshotStore(dataDir, 2, os.Stderr)
	if err != nil {
		return nil, "", err
	}

	r, err := raft.NewRaft(config, fsm, boltDB, boltDB, snapshotStore, transport)
	if err != nil {
		return nil, "", err
	}

	// Bootstrap o cluster (apenas no primeiro nó)
	if bootstrap {
		log.Println("Iniciando o nó como bootstrap...")
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      config.LocalID,
					Address: transport.LocalAddr(),
				},
			},
		}
		r.BootstrapCluster(configuration)
	}

	return r, raftAddr, nil 
}

// joinCluster tenta se juntar a um cluster Raft existente
func joinCluster(id string, raftPort int, httpPort int, peersStr string) {
	peers := strings.Split(peersStr, ",")
	localRaftAddr := fmt.Sprintf("127.0.0.1:%d", raftPort)

	for _, peer := range peers {
		// Conecta ao endpoint HTTP do peer
		// Assumimos que o peer também tem a porta HTTP aberta no mesmo número (8080, 8081, etc.)
		peerHost := strings.Split(peer, ":")[0]
		joinAddr := fmt.Sprintf("http://%s:%d/join", peerHost, httpPort) 
		
		// O endereço enviado é a porta RPC do nosso nó para que o líder possa se comunicar de volta
		payload := fmt.Sprintf(`{"id": "%s", "address": "%s"}`, id, localRaftAddr)
		
		log.Printf("Tentando fazer join no cluster via %s...", joinAddr)

		resp, err := http.Post(joinAddr, "application/json", strings.NewReader(payload))
		if err != nil {
			log.Printf("Erro ao tentar fazer join com o peer %s: %v. Tentando o próximo...", peer, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			log.Printf("Nó %s se juntou ao cluster via %s com sucesso!", id, peer)
			return
		}

		// Se o nó não for o líder, ele pode retornar 307
		log.Printf("Falha ao se juntar ao peer %s. Status: %d. Tentando o próximo peer...", peer, resp.StatusCode)
	}
    log.Fatalf("Falha ao se juntar a todos os peers conhecidos. Iniciando em modo isolado.")
}