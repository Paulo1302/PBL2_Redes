package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
    "io"
	"os"
	"os/signal"
	"path/filepath"
    "math/rand" // NOVO: Para Timeouts Randomizados
	"strings"
	"syscall"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
)

// Inicialização randômica para Election Timeout
func init() {
    rand.Seed(time.Now().UnixNano())
}

// --- 1. RAFT FSM & STORE (Atualizado) ---

type command struct {
	Op    string `json:"op"`
	Key   string `json:"key"`
	Value string `json:"value"`
    MemberID   string `json:"member_id"`
    MemberAddr string `json:"member_addr"`
}

// CORREÇÃO 3: Estrutura Store com mapa de membros para sincronização via FSM
type Store struct {
	mu       sync.Mutex 
	data     map[string]string
    members  map[string]raft.ServerAddress // NOVO: Lista sincronizada de membros
	RaftLog  *raft.Raft  
	NodeID   string      
	RaftAddr string      
}

func NewStore() *Store {
	return &Store{
		data: make(map[string]string),
        members: make(map[string]raft.ServerAddress),
	}
}

// Implementação da interface raft.FSM - Apply
func (s *Store) Apply(log *raft.Log) interface{} { 
    var c command
    // ... (unmarshal, lock/unlock) ...
    // NOTE: A implementação completa deve garantir que ADD/REMOVE MEMBER apliquem na FSM
    if err := json.Unmarshal(log.Data, &c); err != nil {
        return fmt.Errorf("failed to unmarshal command: %w", err)
    }

    s.mu.Lock()
    defer s.mu.Unlock()

    switch c.Op {
    case "set":
        s.data[c.Key] = c.Value
        return nil
    case "delete":
        delete(s.data, c.Key)
        return nil
    // CORREÇÃO 3: Comandos de sincronização de membros
    case "add_member":
        s.members[c.MemberID] = raft.ServerAddress(c.MemberAddr)
        return nil
    case "remove_member":
        delete(s.members, c.MemberID)
        return nil
    default:
        return fmt.Errorf("unrecognized command op: %s", c.Op)
    }
}

// Snapshot cria um snapshot consistente do estado atual do Store.
// Isso implementa raft.FSM.Snapshot.
func (s *Store) Snapshot() (raft.FSMSnapshot, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    // Faz cópias para evitar que alterações concorrentes afetem o snapshot
    dataCopy := make(map[string]string, len(s.data))
    for k, v := range s.data {
        dataCopy[k] = v
    }
    membersCopy := make(map[string]raft.ServerAddress, len(s.members))
    for k, v := range s.members {
        membersCopy[k] = v
    }

    return &FSMSnapshot{
        Data:    dataCopy,
        Members: membersCopy,
    }, nil
}

// Restore restaura o estado do Store a partir de um snapshot.
// Deve corresponder ao que Persist escreve no FSMSnapshot.
func (s *Store) Restore(rc io.ReadCloser) error { 
    defer rc.Close()
    decoder := json.NewDecoder(rc)
    
    // Struct definition must match the snapshot Persist structure
    var snapshotData struct {
        Data    map[string]string
        Members map[string]raft.ServerAddress
    }
    
    if err := decoder.Decode(&snapshotData); err != nil {
        return err
    }
    s.mu.Lock()
    s.data = snapshotData.Data
    s.members = snapshotData.Members
    s.mu.Unlock()
    return nil
}

// FSMSnapshot representa um snapshot persistente do estado do Store.
// Implementa raft.FSMSnapshot (Persist e Release).
type FSMSnapshot struct {
    Data    map[string]string
    Members map[string]raft.ServerAddress
}

func (f *FSMSnapshot) Persist(sink raft.SnapshotSink) error {
    // Escreve o snapshot como JSON no sink.
    encoder := json.NewEncoder(sink)
    payload := struct {
        Data    map[string]string              `json:"data"`
        Members map[string]raft.ServerAddress  `json:"members"`
    }{
        Data:    f.Data,
        Members: f.Members,
    }

    if err := encoder.Encode(&payload); err != nil {
        _ = sink.Cancel()
        return err
    }

    if err := sink.Close(); err != nil {
        _ = sink.Cancel()
        return err
    }
    return nil
}

func (f *FSMSnapshot) Release() {
    // No-op: não há recursos a liberar explicitamente aqui.
}

// --- 2. HANDLERS E ROUTER (Atualizado) ---

// CORREÇÃO 5: Endpoints REST Completos (Resposta com Líder)
func (s *Store) statusHandler(c *gin.Context) {
	state := s.RaftLog.State().String()
	leader := s.RaftLog.Leader()
    s.mu.Lock()
    members := s.members
    s.mu.Unlock()
    
	c.JSON(http.StatusOK, gin.H{
		"node_id": s.NodeID,
		"state":   state,
		"leader":  leader,
		"address": s.RaftAddr,
        "members": members,
	})
}

// ... (dentro de main.go)

func (s *Store) joinHandler(c *gin.Context) {
    var req struct {
        ID      string `json:"id" binding:"required"`
        Address string `json:"address" binding:"required"`
    }
    
    // 1. Parse da Requisição
    if err := c.BindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
        return
    }

    if s.RaftLog.State() != raft.Leader {
        // Redirecionamento se não for o líder
        c.JSON(http.StatusTemporaryRedirect, gin.H{
            "error": "Not the leader",
            "leader": string(s.RaftLog.Leader()),
        })
        return
	}
    
    // --- LÓGICA CRÍTICA DE RAFT/FSM ---
    
    // 2. Adicionar como Votante (AddVoter) no Cluster Raft
    // A porta 7001 (RaftAddr) é usada aqui, o que está correto.
    f := s.RaftLog.AddVoter(raft.ServerID(req.ID), raft.ServerAddress(req.Address), 0, 0)
    if f.Error() != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to add voter: %v", f.Error())})
        return
    }

    // 3. Aplicar a Mudança na FSM (Sincroniza a lista de membros do Store)
    cmd := command{Op: "add_member", MemberID: req.ID, MemberAddr: req.Address}
    b, err := json.Marshal(cmd)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal FSM command"})
        return
    }
    
    // O Apply retorna um Future. Esperamos pelo resultado.
    fsmApply := s.RaftLog.Apply(b, 500*time.Millisecond)
    if err := fsmApply.Error(); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to apply FSM command: %v", err)})
        return
    }

    // ------------------------------------

    c.JSON(http.StatusOK, gin.H{"status": "Node added successfully to Raft and FSM"})
}

// CORREÇÃO 5: Novo endpoint para Remoção Graciosa (POST /leave)
func (s *Store) leaveHandler(c *gin.Context) {
    if s.RaftLog.State() != raft.Leader {
        c.JSON(http.StatusTemporaryRedirect, gin.H{
            "error": "Not the leader",
            "leader": string(s.RaftLog.Leader()),
        })
        return
	}
    
    // Assume que o ID do nó a ser removido está no corpo da requisição ou URL (Simplificando: usa o ID da requisição)
    var req struct { ID string `json:"id" binding:"required"` }
    if err := c.BindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"}); return }

    // 1. Remove do cluster Raft
    f := s.RaftLog.RemoveServer(raft.ServerID(req.ID), 0, 0)
    if f.Error() != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": f.Error().Error()}); return
    }

    // 2. Remove da FSM (Propagação)
    cmd := command{Op: "remove_member", MemberID: req.ID}
    b, _ := json.Marshal(cmd)
    s.RaftLog.Apply(b, 500*time.Millisecond)

    c.JSON(http.StatusOK, gin.H{"status": "Node removed successfully from Raft and FSM"})
}


func SetupRouter(s *Store) *gin.Engine {
	r := gin.Default()
	r.GET("/status", s.statusHandler)
	r.POST("/join", s.joinHandler)
    r.POST("/leave", s.leaveHandler) // CORREÇÃO 5: Novo endpoint de remoção
	return r
}

// --- 3. FUNÇÕES DE ORQUESTRAÇÃO ---

// CORREÇÃO 2: Função para Health Check TCP
func healthCheckPeer(address string) bool {
    // Timeout fixo de 2 segundos para o health check
    conn, err := net.DialTimeout("tcp", address, 2*time.Second) 
    if err != nil { 
        return false 
    }
    conn.Close()
    return true
}

func setupRaft(id string, port int, fsm *Store, bootstrap bool) (*raft.Raft, string, error) {
    config := raft.DefaultConfig()
    // CORREÇÃO 1: Timeouts Randomizados
    baseTimeout := 1000 * time.Millisecond
    randomOffset := time.Duration(rand.Intn(500)) * time.Millisecond 
    config.ElectionTimeout = baseTimeout + randomOffset // [1000ms - 1500ms)
    
    config.HeartbeatTimeout = 1000 * time.Millisecond
    config.CommitTimeout = 50 * time.Millisecond
    // ... (restante do setupRaft inalterado)
    config.LocalID = raft.ServerID(id)
    config.Logger = hclog.New(&hclog.LoggerOptions{Name: id, Level: hclog.Info})

    raftAddr := fmt.Sprintf("127.0.0.1:%d", port)
    tcpAddr, err := net.ResolveTCPAddr("tcp", raftAddr)
    if err != nil { return nil, "", err }
    
    transport, err := raft.NewTCPTransport(raftAddr, tcpAddr, 10, 5*time.Second, os.Stderr)
    if err != nil { return nil, "", err }

    dataDir := filepath.Join("data", id)
    if err := os.MkdirAll(dataDir, 0755); err != nil { return nil, "", err }
    
    boltDB, err := raftboltdb.NewBoltStore(filepath.Join(dataDir, "raft.db"))
    if err != nil { return nil, "", err }

    snapshotStore, err := raft.NewFileSnapshotStore(dataDir, 2, os.Stderr)
    if err != nil { return nil, "", err }

    r, err := raft.NewRaft(config, fsm, boltDB, boltDB, snapshotStore, transport)
    if err != nil { return nil, "", err }

    if bootstrap {
        log.Println("Iniciando o nó como bootstrap...")
        configuration := raft.Configuration{
            Servers: []raft.Server{
                { ID: config.LocalID, Address: transport.LocalAddr() },
            },
        }
        r.BootstrapCluster(configuration)
    }
    return r, raftAddr, nil
}


// CORREÇÃO 2: Monitoramento de Peers com Health Check Duplo
func monitorRaftPeers(s *Store) {
    ticker := time.NewTicker(30 * time.Second) // Check a cada 30s
    defer ticker.Stop()

    for range ticker.C {
        if s.RaftLog.State() != raft.Leader {
            continue
        }

        cfg := s.RaftLog.GetConfiguration()
        if err := cfg.Error(); err != nil { continue }
        
        for _, server := range cfg.Configuration().Servers {
            if server.ID == raft.ServerID(s.NodeID) { continue }
            
            // CORREÇÃO 2: Health Check Duplo
            if !healthCheckPeer(string(server.Address)) {
                log.Printf("Monitoramento: Peer %s inativo (Primeira Falha). Aguardando retry...", server.ID)
                time.Sleep(5 * time.Second) // Aguarda 5s para falhas temporárias

                if !healthCheckPeer(string(server.Address)) {
                    log.Printf("Monitoramento: Nó %s falhou no Health Check Duplo. Removendo...", server.ID)
                    
                    // 1. Remove do cluster Raft
                    s.RaftLog.RemoveServer(server.ID, 0, 0)
                    
                    // 2. Propaga a remoção na FSM
                    cmd := command{Op: "remove_member", MemberID: string(server.ID)}
                    b, _ := json.Marshal(cmd)
                    s.RaftLog.Apply(b, 500*time.Millisecond)
                }
            }
        }
        
        // Garante que o próprio líder esteja na FSM (CORREÇÃO 3)
        s.mu.Lock()
        if _, ok := s.members[s.NodeID]; !ok {
            cmd := command{Op: "add_member", MemberID: s.NodeID, MemberAddr: s.RaftAddr}
            b, _ := json.Marshal(cmd)
            s.RaftLog.Apply(b, 500*time.Millisecond)
        }
        s.mu.Unlock()
	}
}

// CORREÇÃO 4: Observer de Mudanças de Liderança
func observeLeaderChanges(r *raft.Raft) {
    observations := make(chan raft.Observation, 10)
    
    // O filtro de observação pode ser simplificado se o Raft.StateChange não for encontrado
    // Ou podemos filtrar explicitamente por LeaderObservation.
    observer := raft.NewObserver(observations, false, func(o *raft.Observation) bool {
        // Observar apenas LeaderObservation. O Raft trata as demais transições internamente.
        _, isLeader := o.Data.(raft.LeaderObservation)
        // Removendo a verificação raft.StateChange para resolver o 'undefined'
        return isLeader 
    })
    r.RegisterObserver(observer)

    for obs := range observations {
        if leaderObs, ok := obs.Data.(raft.LeaderObservation); ok {
            log.Printf("🔄 Mudança: Novo líder = %s (Endereço: %s)", leaderObs.LeaderID, leaderObs.Leader)
        } 
        // A lógica de else if stateObs, ok := obs.Data.(raft.StateChange); ok { foi removida
    }
}

func joinCluster(id string, raftPort int, leaderHTTPPort int, peersStr string) { 
    peers := strings.Split(peersStr, ",")
    localRaftAddr := fmt.Sprintf("127.0.0.1:%d", raftPort) // Variável agora usada no payload
    
    // Configuração do Cliente HTTP para seguir redirecionamentos
    // Isso é crucial para que o nó se junte automaticamente ao líder correto,
    // mesmo se o peer que ele contatar for um follower.
    client := &http.Client{
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            if len(via) >= 5 {
                // Previne loops de redirecionamento infinitos
                return http.ErrUseLastResponse
            }
            return nil
        },
    }

    for _, peer := range peers {
        // O peerStr pode ter a porta Raft, mas precisamos da porta HTTP do líder (leaderHTTPPort)
        peerHost := strings.Split(peer, ":")[0]
        
        joinAddr := fmt.Sprintf("http://%s:%d/join", peerHost, leaderHTTPPort) 
        
        // CORREÇÃO: Uso da variável localRaftAddr no payload
        payload := fmt.Sprintf(`{"id": "%s", "address": "%s"}`, id, localRaftAddr)
        
        resp, err := client.Post(joinAddr, "application/json", strings.NewReader(payload))
        if err != nil {
            log.Printf("Erro ao tentar fazer join com o peer %s: %v. Tentando o próximo...", peer, err)
            continue
        }
        
        // O defer deve ser chamado dentro do loop, garantindo que o body seja fechado a cada iteração.
        defer resp.Body.Close()

        if resp.StatusCode == http.StatusOK {
            log.Printf("Nó %s se juntou ao cluster via %s com sucesso!", id, peer)
            // Se necessário, você pode adicionar aqui o Health Check Duplo/verificação da FSM
            // que foi planejado nas correções anteriores.
            return
        }
        
        if resp.StatusCode == http.StatusTemporaryRedirect {
            log.Printf("Peer %s redirecionou (código %d). O cliente deveria ter seguido. Tentando o próximo peer na lista.", peer, resp.StatusCode)
            // O cliente HTTP tenta seguir automaticamente. Aqui, apenas registramos e tentamos o próximo peer
            // se o redirecionamento final falhar (o que será pego pelo log.Fatalf).
            continue
        }

        log.Printf("Falha ao se juntar ao peer %s. Status: %d. Tentando o próximo peer...", peer, resp.StatusCode)
    }
    log.Fatalf("Falha ao se juntar a todos os peers conhecidos. Iniciando em modo isolado.")
}

// --- 4. FUNÇÃO MAIN (Ponto de Entrada) ---
func main() {
	var (
		nodeID    string
		httpPort  int
		raftPort  int
		bootstrap bool
		peersStr  string
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

	store := NewStore()
    
    var raftLog *raft.Raft 
    var raftAddr string
    var err error

	// 3. Configura o Raft
	raftLog, raftAddr, err = setupRaft(nodeID, raftPort, store, bootstrap) 
	if err != nil {
		log.Fatalf("Erro ao configurar o Raft: %v", err)
	}

	// 4. Preenche os campos do Store
	store.RaftLog = raftLog
	store.NodeID = nodeID
	store.RaftAddr = raftAddr
    
    // Inicia o observer de mudanças de liderança (CORREÇÃO 4)
    go observeLeaderChanges(raftLog)

    // Inicia a rotina de monitoramento.
    go monitorRaftPeers(store)

	log.Printf("Nó Raft (%s) inicializado em %s", store.NodeID, store.RaftAddr)

	// 5. Lógica de Join
	if !bootstrap && peersStr != "" {
        const leaderHTTPPort = 8080 
		joinCluster(nodeID, raftPort, leaderHTTPPort, peersStr) 
	}

	// 6. Inicia o Servidor HTTP (API REST)
	router := SetupRouter(store)
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

	// 7. Tratamento de Shutdown (CORREÇÃO 5: Shutdown ordenado)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Encerrando o servidor...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Shutdown do HTTP e RAFT
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Erro no shutdown do servidor HTTP: %v", err)
	}

	if raftLog != nil {
		// A remoção graciosa via Raft/FSM (POST /leave) deve ser feita pelo cliente
		// antes do shutdown se o nó não for o Líder.
		
		if err := raftLog.Shutdown().Error(); err != nil {
			log.Printf("Erro no shutdown do Raft: %v", err)
		}
	}

	log.Println("Servidor interrompido com sucesso.")
}