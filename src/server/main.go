package main

import (
	"context"
	"encoding/json" // Mantido para monitorRaftPeers e joinCluster
	"flag"
	"fmt"
	"io" // Mantido para joinCluster
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	// Importa o pacote API refatorado
	"server/API" // Certifique-se que o path do módulo está correto no seu go.mod
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
)


// healthCheckPeer verifica se um nó está acessível via TCP.
func healthCheckPeer(address string) bool {
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// setupRaft configura e inicializa a instância do Raft.
// A assinatura agora usa *API.Store para fsm
func setupRaft(id string, port int, advertiseAddr string, fsm *API.Store, bootstrap bool) (*raft.Raft, error) {
	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(id)

	baseTimeout := 1000 * time.Millisecond
	randomOffset := time.Duration(rand.Intn(500)) * time.Millisecond
	config.ElectionTimeout = baseTimeout + randomOffset
	config.HeartbeatTimeout = 1000 * time.Millisecond
	config.CommitTimeout = 50 * time.Millisecond

	config.Logger = hclog.New(&hclog.LoggerOptions{
		Name:  fmt.Sprintf("raft-%s", id),
		Level: hclog.Info,
	})

	bindAddr := fmt.Sprintf("0.0.0.0:%d", port)

	advAddr, err := net.ResolveTCPAddr("tcp", advertiseAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve advertise address '%s': %w", advertiseAddr, err)
	}

	transport, err := raft.NewTCPTransport(bindAddr, advAddr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create TCP transport: %w", err)
	}
	log.Printf("Raft transport created. Bind: %s, Advertise: %s\n", bindAddr, advAddr.String())

	dataDir := filepath.Join("data", id)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory '%s': %w", dataDir, err)
	}
	log.Printf("Data directory ensured: %s\n", dataDir)

	boltDBPath := filepath.Join(dataDir, "raft.db")
	boltStore, err := raftboltdb.NewBoltStore(boltDBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create bolt store at '%s': %w", boltDBPath, err)
	}
	log.Printf("BoltDB store created at: %s\n", boltDBPath)

	snapshotStore, err := raft.NewFileSnapshotStore(dataDir, 2, os.Stderr)
	if err != nil {
		boltStore.Close()
		return nil, fmt.Errorf("failed to create file snapshot store in '%s': %w", dataDir, err)
	}
	log.Printf("File snapshot store created in: %s\n", dataDir)

	// Cria a instância Raft, passando a FSM do pacote API
	raftNode, err := raft.NewRaft(config, fsm, boltStore, boltStore, snapshotStore, transport)
	if err != nil {
		boltStore.Close()
		return nil, fmt.Errorf("failed to create raft instance: %w", err)
	}
	log.Println("Raft instance created successfully")

	// Bootstrap se necessário
	if bootstrap {
		log.Println("Bootstrapping cluster as the first node...")
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      config.LocalID,
					Address: transport.LocalAddr(),
				},
			},
		}
		bootstrapFuture := raftNode.BootstrapCluster(configuration)
		if err := bootstrapFuture.Error(); err != nil {
			raftNode.Shutdown()
			boltStore.Close()
			return nil, fmt.Errorf("failed to bootstrap cluster: %w", err)
		}
		log.Println("Cluster bootstrapped successfully")
		// NOTA: O líder será adicionado à FSM pela goroutine monitorRaftPeers
	}

	return raftNode, nil
}

// monitorRaftPeers verifica periodicamente a saúde dos peers e remove os inativos.
// A assinatura agora usa *API.Store
func monitorRaftPeers(s *API.Store) {
	ticker := time.NewTicker(15 * time.Second) // Check a cada 30 segundos
	defer ticker.Stop()

	for range ticker.C {
		// Apenas o líder executa a monitorização
		if s.RaftLog.State() != raft.Leader {
			continue
		}

		// Obtém a configuração atual do cluster Raft
		cfgFuture := s.RaftLog.GetConfiguration()
		if err := cfgFuture.Error(); err != nil {
			log.Printf("[Monitor] Error getting Raft configuration: %v\n", err)
			continue
		}
		configuration := cfgFuture.Configuration()
		removedCount := 0

		// Obtém a lista de membros da FSM para comparação
		currentMembersFSM := s.GetMembers()

		// Verifica nós na configuração Raft
		for _, server := range configuration.Servers {
			// Não verifica a si mesmo
			if server.ID == raft.ServerID(s.NodeID) {
				continue
			}

			// Executa health check duplo
			if !healthCheckPeer(string(server.Address)) {
				log.Printf("[Monitor] Peer '%s' (%s) failed first health check. Retrying in 5s...\n", server.ID, server.Address)
				time.Sleep(5 * time.Second)

				if !healthCheckPeer(string(server.Address)) {
					log.Printf("[Monitor] Peer '%s' (%s) failed second health check. Removing...\n", server.ID, server.Address)

					// 1. Remove da configuração Raft
					removeFuture := s.RaftLog.RemoveServer(server.ID, 0, 0)
					if err := removeFuture.Error(); err != nil {
						log.Printf("[Monitor] Error removing server '%s' from Raft: %v\n", server.ID, err)
						continue // Tenta o próximo
					}
					log.Printf("[Monitor] Server '%s' successfully removed from Raft configuration.\n", server.ID)
					removedCount++

					// 2. Propaga a remoção para a FSM (remove_member)
					// Usa applyLogInternal (assumindo que seja exportada ou refatorada)
					// Ou monta o payload e chama Apply diretamente
					cmdPayload := map[string]string{
						"op":        "remove_member",
						"member_id": string(server.ID),
					}
					cmdBytes, err := json.Marshal(cmdPayload)
					if err == nil {
						applyFuture := s.RaftLog.Apply(cmdBytes, 500*time.Millisecond)
						if err := applyFuture.Error(); err != nil {
							log.Printf("[Monitor] Error applying FSM remove for '%s': %v\n", server.ID, err)
						} else {
							log.Printf("[Monitor] FSM Apply successful for removing '%s'.\n", server.ID)
						}
					} else {
						log.Printf("[Monitor] Error marshalling FSM remove command for '%s': %v\n", server.ID, err)
					}
				} else {
					log.Printf("[Monitor] Peer '%s' (%s) passed second health check.\n", server.ID, server.Address)
				}
			} // Fim if !healthCheckPeer
		} // Fim for server

		// Garante que o próprio líder está na lista de membros da FSM
		if _, ok := currentMembersFSM[s.NodeID]; !ok {
			log.Printf("[Monitor] Leader '%s' (%s) not found in FSM. Applying add_member.\n", s.NodeID, s.RaftAddr)
			cmdPayload := map[string]string{
				"op":          "add_member",
				"member_id":   s.NodeID,
				"member_addr": s.RaftAddr,
			}
			cmdBytes, err := json.Marshal(cmdPayload)
			if err == nil {
				s.RaftLog.Apply(cmdBytes, 500*time.Millisecond) // Aplica a correção na FSM
			} else {
				log.Printf("[Monitor] Error marshalling FSM command for self-add: %v\n", err)
			}
		}

		if removedCount > 0 {
			log.Printf("[Monitor] Finished health check cycle. Removed %d inactive node(s).\n", removedCount)
		}

	} // Fim for range ticker
}

// observeLeaderChanges observa e regista mudanças na liderança do cluster Raft.
func observeLeaderChanges(r *raft.Raft) {
	observations := make(chan raft.Observation, 10)
	observer := raft.NewObserver(observations, false, func(o *raft.Observation) bool {
		_, isLeaderChange := o.Data.(raft.LeaderObservation)
		return isLeaderChange
	})
	r.RegisterObserver(observer)
	log.Println("Leader change observer registered.")

	for obs := range observations {
		if leaderObs, ok := obs.Data.(raft.LeaderObservation); ok {
			if leaderObs.LeaderID != "" {
				log.Printf("🔄 New Leader Elected: ID='%s', Address='%s'\n", leaderObs.LeaderID, leaderObs.Leader)
			} else {
				log.Println("🔄 Leadership lost. No current leader.")
			}
		}
	}
	log.Println("Leader change observer stopped.") // Não deve ser alcançado
}

// joinCluster tenta juntar este nó a um cluster existente contactando os peers.
func joinCluster(id string, localRaftAddr string, leaderHTTPPort int, peersStr string) {
	peers := strings.Split(peersStr, ",")
	log.Printf("Attempting to join cluster via peers: %v\n", peers)

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Não seguir redirects
		},
	}

	joined := false
	triedPeers := make(map[string]bool)

	for len(peers) > 0 {
		peer := peers[0]
		peers = peers[1:]

		if peer == "" || triedPeers[peer] {
			continue
		}
		triedPeers[peer] = true

		peerHost := strings.Split(peer, ":")[0]
		joinAddr := fmt.Sprintf("http://%s:%d/join", peerHost, leaderHTTPPort)
		payload := fmt.Sprintf(`{"id": "%s", "address": "%s"}`, id, localRaftAddr)
		log.Printf("Attempting join via %s with payload: %s\n", joinAddr, payload)

		resp, err := client.Post(joinAddr, "application/json", strings.NewReader(payload))
		if err != nil {
			log.Printf("Error joining via peer %s (%s): %v. Trying next...\n", peer, joinAddr, err)
			continue
		}

		bodyBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		bodyString := ""
		if readErr == nil {
			bodyString = string(bodyBytes)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			log.Printf("Successfully joined cluster via peer %s (%s)! Response: %s\n", peer, joinAddr, bodyString)
			joined = true
			goto EndJoinLoop

		case http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			leaderAddr := resp.Header.Get("Location")
			if leaderAddr == "" {
				var jsonResp map[string]string
				if json.Unmarshal(bodyBytes, &jsonResp) == nil {
					leaderAddr = jsonResp["leader"]
				}
			}

			if leaderAddr != "" && !triedPeers[leaderAddr] {
				log.Printf("Peer %s (%s) redirected to leader at %s. Adding leader to peer list.\n", peer, joinAddr, leaderAddr)
				peers = append(peers, leaderAddr)
			} else if leaderAddr != "" && triedPeers[leaderAddr] {
				log.Printf("Peer %s (%s) redirected to already tried leader %s.\n", peer, joinAddr, leaderAddr)
			} else {
				log.Printf("Peer %s (%s) redirected (Status %d) but did not provide leader address. Response: %s\n", peer, joinAddr, resp.StatusCode, bodyString)
			}

		default:
			log.Printf("Failed to join via peer %s (%s). Status: %d. Response: %s. Trying next...\n", peer, joinAddr, resp.StatusCode, bodyString)
		}
	} // Fim do loop for (while)

EndJoinLoop:
	if !joined {
		log.Fatal("FATAL: Failed to join cluster after trying all known peers and redirects. Exiting.")
	}
	log.Println("Join process completed.")
}

func main() {
	// Definição das flags de linha de comando
	var (
		nodeID    string
		httpPort  int
		raftPort  int
		bootstrap bool
		peersStr  string
		raftAddr  string // Endereço que este nó anunciará aos outros
	)

	flag.StringVar(&nodeID, "id", "", "ID exclusivo do nó Raft (obrigatório)")
	flag.IntVar(&httpPort, "port", 8080, "Porta HTTP para API REST")
	flag.IntVar(&raftPort, "raft-port", 7000, "Porta RPC para comunicação Raft interna")
	flag.BoolVar(&bootstrap, "bootstrap", false, "Iniciar como o primeiro nó (líder) do cluster")
	flag.StringVar(&peersStr, "peers", "", "Lista separada por vírgulas de peers para tentar join (ex: 'host1:7000,host2:7000')")
	flag.StringVar(&raftAddr, "raft-addr", "", "Endereço Raft anunciável (IP:PORTA) (obrigatório, ex: --raft-addr=192.168.1.10:7000)")
	flag.Parse()

	// Validação das flags obrigatórias
	if nodeID == "" {
		log.Fatal("O ID do nó (--id) deve ser fornecido.")
	}
	if raftAddr == "" {
		log.Fatal("O endereço Raft (--raft-addr) deve ser fornecido (ex: --raft-addr=192.168.1.10:7000).")
	}

	log.Printf("Starting Raft node with config: ID=%s, HTTP Port=%d, Raft Port=%d, Bootstrap=%t, Peers=%s, Raft Addr=%s\n",
		nodeID, httpPort, raftPort, bootstrap, peersStr, raftAddr)

	// *** Inicializa o Store (FSM) usando o pacote API ***
	store := API.NewStore()

	// Inicializa NATS (sem bloquear o arranque do Raft)
	go func() {
		API.SetupPS(store)
		log.Println("NATS Pub/Sub initialized successfully.")
	}()

	// Configura e inicializa a instância do Raft
	// Passa o *API.Store como a FSM
	raftNode, err := setupRaft(nodeID, raftPort, raftAddr, store, bootstrap)
	if err != nil {
		log.Fatalf("FATAL: Error setting up Raft: %v", err)
	}

	// Preenche os campos restantes no Store com informações pós-inicialização do Raft
	// Estes campos permitem que os handlers da API acedam à instância Raft e à config do nó
	store.RaftLog = raftNode
	store.NodeID = nodeID
	store.RaftAddr = raftAddr // Confirma o endereço anunciado

	// Inicia goroutines para observar mudanças de liderança e monitorar peers
	go observeLeaderChanges(raftNode)
	go monitorRaftPeers(store) // Passa o *API.Store

	log.Printf("Raft node '%s' initialized successfully at %s\n", store.NodeID, store.RaftAddr)

	// Se não for o nó de bootstrap e houver peers, tenta juntar-se ao cluster
	if !bootstrap && peersStr != "" {
		const leaderHTTPPort = 8080 // Assume que a API do líder está sempre na 8080
		// Passa o endereço Raft local para a função de join
		joinCluster(nodeID, raftAddr, leaderHTTPPort, peersStr)
		// Se joinCluster falhar, ele chama log.Fatal e encerra o programa
	} else if !bootstrap && peersStr == "" {
		log.Println("WARN: Starting as a non-bootstrap node without peers. Will remain isolated until joined manually via API.")
	}

	// *** Configura e inicia o servidor HTTP (API REST) usando o SetupRouter do pacote API ***
	router := API.SetupRouter(store) // Chama a função exportada do pacote API
	httpAddr := fmt.Sprintf(":%d", httpPort)
	log.Printf("Starting HTTP API server on %s\n", httpAddr)

	httpServer := &http.Server{
		Addr:    httpAddr,
		Handler: router, // Usa o router configurado pelo pacote API
	}

	// Inicia o servidor HTTP numa goroutine separada
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("FATAL: Failed to start HTTP server: %v", err)
		}
	}()
	

	fmt.Println("A: ", store)
	// Aguarda por sinais de interrupção (Ctrl+C) para shutdown gracioso
	log.Println("Server started. Waiting for interrupt signal (Ctrl+C)...")
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit // Bloqueia até receber o sinal

	// Inicia o processo de shutdown
	log.Println("Shutdown signal received. Shutting down server gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // Timeout para shutdown
	defer cancel()

	// 1. Shutdown do servidor HTTP
	log.Println("Shutting down HTTP server...")
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("WARN: Error during HTTP server shutdown: %v\n", err)
	} else {
		log.Println("HTTP server shut down successfully.")
	}

	// 2. Shutdown do nó Raft
	if raftNode != nil {
		log.Println("Shutting down Raft node...")
		// A remoção do cluster (se não for líder) deve ser feita via API ANTES do shutdown
		if err := raftNode.Shutdown().Error(); err != nil {
			log.Printf("WARN: Error during Raft node shutdown: %v\n", err)
		} else {
			log.Println("Raft node shut down successfully.")
		}
	}

	log.Println("Server shutdown complete.")
}

