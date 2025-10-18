package API

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hashicorp/raft"
)

// ApplyLog envia um comando (log) para o líder Raft.
func (s *Store) ApplyLog(c *gin.Context, op, key, value string) (interface{}, error) {
	if s.RaftLog.State() != raft.Leader {
		return nil, gin.Error{Err: http.ErrAbortHandler, Type: http.StatusPermanentRedirect, Meta: "Node is not the leader"}
	}

	cmd := command{Op: op, Key: key, Value: value}
	b, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	// Envia o log para o cluster Raft
	f := s.RaftLog.Apply(b, 500*time.Millisecond)
	if err := f.Error(); err != nil {
		return nil, err
	}

	return f.Response(), nil
}

// getHandler retorna um valor da FSM
func (s *Store) getHandler(c *gin.Context) {
    // ... (Lógica getHandler inalterada) ...
	key := c.Param("key")

	s.mu.Lock()
	defer s.mu.Unlock()

	if value, ok := s.data[key]; ok {
		c.JSON(http.StatusOK, gin.H{"key": key, "value": value})
		return
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "key not found"})
}

// setHandler aplica um novo valor através do Raft
func (s *Store) setHandler(c *gin.Context) {
    // ... (Lógica setHandler inalterada) ...
	var req struct {
		Value string `json:"value" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}

	key := c.Param("key")

	_, err := s.ApplyLog(c, "set", key, req.Value)
	if err != nil {
		// Se não for o líder, tenta redirecionar
		if e, ok := err.(gin.Error); ok && e.Type == http.StatusPermanentRedirect {
			c.JSON(http.StatusTemporaryRedirect, gin.H{"error": "Not the leader. Use the leader for writing operations."})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "applied", "key": key, "value": req.Value})
}

// statusHandler retorna o status atual do nó Raft (Leader/Follower)
func (s *Store) statusHandler(c *gin.Context) {
	state := s.RaftLog.State().String()
	leader := s.RaftLog.Leader()
    
    // CORREÇÃO: Inclui a lista de membros na resposta
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


// joinHandler é para nós que não são bootstrap se juntarem ao cluster
func (s *Store) joinHandler(c *gin.Context) {
	var req struct {
		ID      string `json:"id" binding:"required"`
		Address string `json:"address" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid join request"})
		return
	}

	if s.RaftLog.State() != raft.Leader {
		c.JSON(http.StatusTemporaryRedirect, gin.H{"error": "Only the leader can add peers."})
		return
	}

	// 1. Adiciona o novo peer ao cluster Raft (Configuração)
	f := s.RaftLog.AddVoter(raft.ServerID(req.ID), raft.ServerAddress(req.Address), 0, 0)
	if f.Error() != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": f.Error().Error()})
		return
	}
    
    // 2. CORREÇÃO: Aplica a alteração na FSM (Lista de Membros)
    cmd := command{
        Op: "add_member", 
        MemberID: req.ID, 
        MemberAddr: req.Address,
    }
    b, _ := json.Marshal(cmd)
    
    // Aplica o log para garantir que a lista de membros seja replicada
    applyFSM := s.RaftLog.Apply(b, 500*time.Millisecond)
    if err := applyFSM.Error(); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to apply member change to FSM."})
        return
    }

	c.JSON(http.StatusOK, gin.H{"status": "Node added successfully to Raft and FSM"})
}

// membersHandler retorna a lista sincronizada de membros da FSM (Item 6)
func (s *Store) membersHandler(c *gin.Context) {
    // Redireciona leituras para o líder para garantir consistência
    if s.RaftLog.State() != raft.Leader {
        c.JSON(http.StatusTemporaryRedirect, gin.H{"error": "Redirect to leader for member list."})
        return
    }

    s.mu.Lock()
    defer s.mu.Unlock()
    
    c.JSON(http.StatusOK, gin.H{
        "leader_id": s.NodeID,
        "members": s.members,
    })
}

// SetupRouter configura as rotas HTTP e é público (exportado)
func SetupRouter(s *Store) *gin.Engine {
	r := gin.Default()
	r.GET("/status", s.statusHandler)
	r.POST("/join", s.joinHandler)
	r.GET("/data/:key", s.getHandler)
	r.POST("/data/:key", s.setHandler)
    r.GET("/members", s.membersHandler) // CORREÇÃO: Novo endpoint para membros
	return r
}