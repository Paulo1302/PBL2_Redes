package API

import (
	"encoding/json"
	"fmt" // Adicionado para logs
	"log" // Adicionado para logs
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hashicorp/raft"
)

// ApplyLog envia um comando (log) para o líder Raft.
// Esta é uma função auxiliar interna ao pacote API.
func (s *Store) applyLogInternal(op string, key string, value string, memberID string, memberAddr string) (interface{}, error) {
	if s.RaftLog.State() != raft.Leader {
		// Retorna um erro específico que pode ser interpretado pelo handler
		return nil, fmt.Errorf("node is not the leader")
	}

	// Usa o 'command' struct definido em store.go (interno ao pacote API)
	cmd := command{
		Op:         op,
		Key:        key,
		Value:      value,
		MemberID:   memberID,
		MemberAddr: memberAddr,
	}
	b, err := json.Marshal(cmd)
	if err != nil {
		log.Printf("Error marshalling command (%s): %v\n", op, err)
		return nil, fmt.Errorf("failed to marshal command: %w", err)
	}

	// Envia o log para o cluster Raft com um timeout
	applyFuture := s.RaftLog.Apply(b, 500*time.Millisecond)
	if err := applyFuture.Error(); err != nil {
		log.Printf("Error applying command (%s) to Raft: %v\n", op, err)
		return nil, fmt.Errorf("failed to apply command to Raft: %w", err)
	}

	// Retorna a resposta da FSM (se houver) ou nil
	return applyFuture.Response(), nil
}

// ---------------- Handlers (Métodos em *Store) -------------------

// getHandler retorna um valor da FSM local (leitura)
func (s *Store) getHandler(c *gin.Context) {
	key := c.Param("key")

	// Leituras podem ser feitas em qualquer nó, mas para consistência forte,
	// pode-se preferir redirecionar para o líder ou usar ReadOnly (não implementado aqui).
	s.mu.Lock()
	value, ok := s.data[key]
	s.mu.Unlock()

	if ok {
		c.JSON(http.StatusOK, gin.H{"key": key, "value": value})
	} else {
		c.JSON(http.StatusNotFound, gin.H{"error": "key not found"})
	}
}

// setHandler aplica um novo valor através do Raft (escrita)
func (s *Store) setHandler(c *gin.Context) {
	var req struct {
		Value string `json:"value" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload: 'value' is required"})
		return
	}

	key := c.Param("key")

	// Tenta aplicar o log via Raft
	_, err := s.applyLogInternal("set", key, req.Value, "", "")
	if err != nil {
		// Verifica se o erro foi por não ser o líder
		if err.Error() == "node is not the leader" {
			leaderAddr := string(s.RaftLog.Leader())
			log.Printf("[API Set] Not leader, redirecting client to leader: %s\n", leaderAddr)
			c.JSON(http.StatusTemporaryRedirect, gin.H{
				"error":  "Not the leader. Redirect to the leader for write operations.",
				"leader": leaderAddr,
			})
		} else {
			// Outro erro durante o Apply
			log.Printf("[API Set] Error applying log for key '%s': %v\n", key, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to apply log: %v", err)})
		}
		return
	}

	// Se chegou aqui, o Apply foi bem-sucedido
	log.Printf("[API Set] Successfully applied 'set' for key '%s'\n", key)
	c.JSON(http.StatusOK, gin.H{"status": "applied", "key": key, "value": req.Value})
}

// statusHandler retorna o status atual do nó Raft
func (s *Store) statusHandler(c *gin.Context) {
	state := s.RaftLog.State().String()
	leader := s.RaftLog.Leader()

	// Usa o método GetMembers() para obter cópia segura da lista
	members := s.GetMembers()

	c.JSON(http.StatusOK, gin.H{
		"node_id": s.NodeID,
		"state":   state,
		"leader":  leader,
		"address": s.RaftAddr,
		"members": members, // Inclui a lista de membros da FSM
	})
}

// joinHandler lida com pedidos de nós para se juntarem ao cluster
func (s *Store) joinHandler(c *gin.Context) {
	var req struct {
		ID      string `json:"id" binding:"required"`
		Address string `json:"address" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid join request: 'id' and 'address' are required"})
		return
	}

	// Apenas o líder pode adicionar nós
	if s.RaftLog.State() != raft.Leader {
		leaderAddr := string(s.RaftLog.Leader())
		log.Printf("[API Join] Not leader, redirecting join request from '%s' to leader: %s\n", req.ID, leaderAddr)
		c.JSON(http.StatusTemporaryRedirect, gin.H{
			"error":  "Not the leader. Redirect to the leader to join.",
			"leader": leaderAddr,
		})
		return
	}

	log.Printf("[API Join] Received join request from Node '%s' at '%s'\n", req.ID, req.Address)

	// 1. Adiciona o nó à configuração do Raft (AddVoter)
	addVoterFuture := s.RaftLog.AddVoter(raft.ServerID(req.ID), raft.ServerAddress(req.Address), 0, 0)
	if err := addVoterFuture.Error(); err != nil {
		log.Printf("[API Join] Error adding voter '%s' to Raft config: %v\n", req.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to add voter to Raft configuration: %v", err)})
		return
	}
	log.Printf("[API Join] Raft AddVoter successful for Node '%s'\n", req.ID)

	// 2. Aplica a adição à FSM (add_member) via Raft
	_, err := s.applyLogInternal("add_member", "", "", req.ID, req.Address)
	if err != nil {
		// Se falhar aqui, o nó está na config Raft mas não na FSM. Potencial inconsistência.
		// Poderia tentar remover o Voter aqui como compensação, mas é complexo.
		log.Printf("[API Join] Error applying 'add_member' to FSM for Node '%s': %v (Raft AddVoter succeeded)\n", req.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to apply member addition to FSM: %v", err)})
		return
	}

	log.Printf("[API Join] FSM 'add_member' successfully applied for Node '%s'\n", req.ID)
	c.JSON(http.StatusOK, gin.H{"status": "Node added successfully to Raft and FSM"})
}

// leaveHandler lida com pedidos para remover nós do cluster
// (Copiado da sua versão de main.go, assumindo que está correto)
func (s *Store) leaveHandler(c *gin.Context) {
	if s.RaftLog.State() != raft.Leader {
		leaderAddr := string(s.RaftLog.Leader())
		log.Printf("[API Leave] Not leader, redirecting leave request to leader: %s\n", leaderAddr)
		c.JSON(http.StatusTemporaryRedirect, gin.H{
			"error":  "Not the leader. Redirect to the leader to leave.",
			"leader": leaderAddr,
		})
		return
	}

	var req struct {
		ID string `json:"id" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid leave request: 'id' is required"})
		return
	}

	log.Printf("[API Leave] Received leave request for Node '%s'\n", req.ID)

	// 1. Remove o nó da configuração do Raft (RemoveServer)
	removeFuture := s.RaftLog.RemoveServer(raft.ServerID(req.ID), 0, 0)
	if err := removeFuture.Error(); err != nil {
		log.Printf("[API Leave] Error removing server '%s' from Raft config: %v\n", req.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to remove server from Raft: %v", err)})
		return
	}
	log.Printf("[API Leave] Raft RemoveServer successful for Node '%s'\n", req.ID)

	// 2. Aplica a remoção à FSM (remove_member) via Raft
	_, err := s.applyLogInternal("remove_member", "", "", req.ID, "")
	if err != nil {
		log.Printf("[API Leave] Error applying 'remove_member' to FSM for Node '%s': %v (Raft RemoveServer succeeded)\n", req.ID, err)
		// Continua para retornar sucesso parcial, pois o nó já saiu do Raft
	} else {
		log.Printf("[API Leave] FSM 'remove_member' successfully applied for Node '%s'\n", req.ID)
	}

	c.JSON(http.StatusOK, gin.H{"status": "Node removed successfully from Raft (FSM update applied/attempted)"})
}

// membersHandler retorna a lista de membros da FSM.
// Leitura pode ser feita em qualquer nó (consistência eventual).
func (s *Store) membersHandler(c *gin.Context) {
	// Obtém a cópia segura da lista de membros
	members := s.GetMembers()

	c.JSON(http.StatusOK, gin.H{
		"queried_node_id": s.NodeID,
		"members":         members,
	})
}

// SetupRouter configura as rotas HTTP e retorna o router Gin.
// Esta função é EXPORTADA (começa com letra maiúscula) para ser usada pelo main.go.
func SetupRouter(s *Store) *gin.Engine {
	// gin.SetMode(gin.ReleaseMode) // Descomentar para produção
	r := gin.Default()

	// Define os endpoints
	r.GET("/status", s.statusHandler)         // Status do nó atual
	r.POST("/join", s.joinHandler)           // Pedido para juntar ao cluster
	r.POST("/leave", s.leaveHandler)         // Pedido para sair do cluster
	r.GET("/members", s.membersHandler)       // Lista de membros do cluster (da FSM)
	r.GET("/data/:key", s.getHandler)        // Obter valor da FSM
	r.POST("/data/:key", s.setHandler)       // Definir valor via Raft

	log.Println("Gin router configured successfully.")
	return r
}
