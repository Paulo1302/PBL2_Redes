package API

import (
    "bytes"
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "sync"
    "time"

    "cardGame/common"
    "github.com/gin-gonic/gin"
)

// ServerCommunicator gerencia comunicação entre servidores
type ServerCommunicator struct {
    serverID    string
    knownPeers  map[string]common.ServerInfo
    httpClient  *http.Client
    mutex       sync.RWMutex
    callbacks   map[string]func(common.ServerCommunicationMessage)
    healthStats map[string]time.Time
}

// NewServerCommunicator cria nova instância
func NewServerCommunicator(serverID string) *ServerCommunicator {
    return &ServerCommunicator{
        serverID:   serverID,
        knownPeers: make(map[string]common.ServerInfo),
        httpClient: &http.Client{
            Timeout: 10 * time.Second,
            Transport: &http.Transport{
                MaxIdleConns:    10,
                IdleConnTimeout: 30 * time.Second,
            },
        },
        callbacks:   make(map[string]func(common.ServerCommunicationMessage)),
        healthStats: make(map[string]time.Time),
    }
}

// RegisterPeer adiciona peer com validação
func (sc *ServerCommunicator) RegisterPeer(peer common.ServerInfo) error {
    if peer.ServerID == "" || peer.Address == "" {
        return fmt.Errorf("invalid peer info: missing required fields")
    }

    sc.mutex.Lock()
    defer sc.mutex.Unlock()
    
    sc.knownPeers[peer.ServerID] = peer
    sc.healthStats[peer.ServerID] = time.Now()
    
    log.Printf("Registered peer: %s (%s:%d)", peer.ServerID, peer.Address, peer.HTTPPort)
    return nil
}

// SendMessage envia mensagem com tratamento de erro robusto
func (sc *ServerCommunicator) SendMessage(targetServerID string, msgType string, data interface{}) error {
    sc.mutex.RLock()
    peer, exists := sc.knownPeers[targetServerID]
    sc.mutex.RUnlock()
    
    if !exists {
        return fmt.Errorf("peer %s not found", targetServerID)
    }
    
    // Gerar ID único para mensagem
    messageID, err := generateMessageID()
    if err != nil {
        return fmt.Errorf("failed to generate message ID: %v", err)
    }
    
    message := common.ServerCommunicationMessage{
        Type:      msgType,
        From:      sc.serverID,
        To:        targetServerID,
        Data:      data,
        Timestamp: time.Now(),
        MessageID: messageID,
    }
    
    jsonData, err := json.Marshal(message)
    if err != nil {
        return fmt.Errorf("failed to marshal message: %v", err)
    }
    
    url := fmt.Sprintf("http://%s:%d/api/server/message", peer.Address, peer.HTTPPort)
    
    ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
    defer cancel()
    
    req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
    if err != nil {
        return fmt.Errorf("failed to create request: %v", err)
    }
    
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("X-Server-ID", sc.serverID)
    
    resp, err := sc.httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("failed to send message: %v", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("server returned error status: %d", resp.StatusCode)
    }
    
    log.Printf("Message sent successfully to %s: %s", targetServerID, msgType)
    return nil
}

// RegisterCallback registra callback sem retorno de erro (compatível com server.go)
func (sc *ServerCommunicator) RegisterCallback(msgType string, callback func(common.ServerCommunicationMessage)) {
    sc.mutex.Lock()
    defer sc.mutex.Unlock()
    
    sc.callbacks[msgType] = callback
    log.Printf("Registered callback for message type: %s", msgType)
}

// HandleIncomingMessage processa mensagem recebida (Handler Gin compatível)
func (sc *ServerCommunicator) HandleIncomingMessage(c *gin.Context) {
    var message common.ServerCommunicationMessage
    if err := c.ShouldBindJSON(&message); err != nil {
        log.Printf("Invalid message format: %v", err)
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid message format"})
        return
    }
    
    // Validação básica
    if message.From == "" || message.Type == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "missing required fields"})
        return
    }
    
    // Atualizar estatísticas do peer
    sc.mutex.Lock()
    sc.healthStats[message.From] = time.Now()
    sc.mutex.Unlock()
    
    // Processar callback se existir
    sc.mutex.RLock()
    callback, exists := sc.callbacks[message.Type]
    sc.mutex.RUnlock()
    
    if exists {
        go callback(message)
    } else {
        log.Printf("No callback registered for message type: %s", message.Type)
    }
    
    c.JSON(http.StatusOK, gin.H{
        "status":     "received",
        "message_id": message.MessageID,
        "timestamp":  time.Now(),
    })
}

// GetKnownPeers retorna lista de peers conhecidos
func (sc *ServerCommunicator) GetKnownPeers() []common.ServerInfo {
    sc.mutex.RLock()
    defer sc.mutex.RUnlock()
    
    peers := make([]common.ServerInfo, 0, len(sc.knownPeers))
    for _, peer := range sc.knownPeers {
        peers = append(peers, peer)
    }
    return peers
}

// generateMessageID gera ID único para mensagem
func generateMessageID() (string, error) {
    bytes := make([]byte, 8)
    if _, err := rand.Read(bytes); err != nil {
        return "", err
    }
    return hex.EncodeToString(bytes), nil
}
