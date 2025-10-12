package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"
    
    "cardGame/common"
    "cardGame/server/API"
    "cardGame/server/discovery"

    "github.com/gin-gonic/gin"
)

// GameServer representa o servidor do jogo distribuído
type GameServer struct {
    config          common.ServerConfig
    ginRouter       *gin.Engine
    discovery       *discovery.ManualDiscovery
    communicator    *API.ServerCommunicator
    httpServer      *http.Server
}

// NewGameServer cria nova instância do servidor
func NewGameServer(config common.ServerConfig) *GameServer {
    gs := &GameServer{
        config: config,
    }
    
    // Inicializar componentes
    gs.discovery = discovery.NewManualDiscovery(config.ServerID, config)
    gs.communicator = API.NewServerCommunicator(config.ServerID)
    
    // Configurar descoberta e comunicação
    gs.discovery.RegisterCallback(gs.onPeerDiscovered)
    gs.setupCommunicationCallbacks()
    
    return gs
}

// Start inicia o servidor
func (gs *GameServer) Start() error {
    log.Printf("Starting game server %s", gs.config.ServerID)
    
    // Inicializar discovery
    if err := gs.discovery.StartDiscovery(); err != nil {
        return fmt.Errorf("failed to start discovery: %v", err)
    }
    
    // Configurar rotas HTTP
    gs.setupRoutes()
    
    // Iniciar comunicação cliente-servidor (NATS)
    go gs.startClientCommunication()
    
    // Iniciar servidor HTTP
    gs.httpServer = &http.Server{
        Addr:    fmt.Sprintf(":%d", gs.config.HTTPPort),
        Handler: gs.ginRouter,
    }
    
    go func() {
        if err := gs.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatalf("Failed to start HTTP server: %v", err)
        }
    }()
    
    log.Printf("Game server started on port %d", gs.config.HTTPPort)
    return nil
}

// setupRoutes configura rotas HTTP
func (gs *GameServer) setupRoutes() {
    gs.ginRouter = gin.Default()
    
    // Rotas existentes
    gs.ginRouter.GET("/hello", func(c *gin.Context) {
        c.JSON(http.StatusOK, gin.H{
            "message":   "hello world",
            "server_id": gs.config.ServerID,
            "peers":     len(gs.communicator.GetKnownPeers()),
        })
    })
    
    // Health check
    gs.ginRouter.GET("/health", func(c *gin.Context) {
        c.JSON(http.StatusOK, gin.H{
            "status":    "healthy",
            "server_id": gs.config.ServerID,
            "timestamp": time.Now(),
        })
    })
    
    // Rotas para comunicação entre servidores
    api := gs.ginRouter.Group("/api")
    {
        server := api.Group("/server")
        {
            server.POST("/message", gs.communicator.HandleIncomingMessage)
            server.GET("/info", gs.handleServerInfo)
            server.GET("/peers", gs.handleGetPeers)
            server.POST("/peer", gs.handleAddPeer)
        }
    }
}

// startClientCommunication inicia comunicação via NATS (código existente)
func (gs *GameServer) startClientCommunication() {
    nc, err := API.BrokerConnect(0)
	if err != nil {
		log.Printf("Failed to connect to NATS broker: %v", err)
		return
	}
    defer nc.Close()
    
    API.ReplyPing(nc)
	
	if err := API.ReplyPing(nc); err != nil {
    log.Printf("Failed to setup ping reply: %v", err)
    return
	}

    select {} // Keep running
}

// onPeerDiscovered callback quando novo peer é descoberto
func (gs *GameServer) onPeerDiscovered(peer common.ServerInfo, isNew bool) {
    if !isNew {
        return
    }
    
    log.Printf("Discovered new peer: %s (%s:%d)", peer.ServerID, peer.Address, peer.HTTPPort)
    
    // Registrar peer no comunicador
    gs.communicator.RegisterPeer(peer)
    
    // Enviar informações próprias para o peer
    go gs.introduceSelfToPeer(peer)
}

// introduceSelfToPeer apresenta este servidor ao peer
func (gs *GameServer) introduceSelfToPeer(peer common.ServerInfo) {
    time.Sleep(2 * time.Second) // Aguardar peer estar pronto
    
    selfInfo := gs.discovery.GetServerInfo()
    err := gs.communicator.SendMessage(peer.ServerID, "server_introduction", selfInfo)
    if err != nil {
        log.Printf("Failed to introduce self to peer %s: %v", peer.ServerID, err)
    } else {
        log.Printf("Successfully introduced self to peer %s", peer.ServerID)
    }
}

// setupCommunicationCallbacks configura callbacks de comunicação
func (gs *GameServer) setupCommunicationCallbacks() {
    // Callback para introdução de servidor
    gs.communicator.RegisterCallback("server_introduction", func(msg common.ServerCommunicationMessage) {
        var peerInfo common.ServerInfo
        if data, ok := msg.Data.(map[string]interface{}); ok {
            // Parse manual dos dados (em produção, usar biblioteca de mapeamento)
            peerInfo = common.ServerInfo{
                ServerID: data["server_id"].(string),
                Address:  data["address"].(string),
                HTTPPort: int(data["http_port"].(float64)),
                RaftPort: int(data["raft_port"].(float64)),
                Status:   "active",
                LastSeen: time.Now(),
            }
            
            gs.discovery.AddPeerManually(peerInfo)
        }
    })
    
    // Callback para heartbeat
    gs.communicator.RegisterCallback("heartbeat", func(msg common.ServerCommunicationMessage) {
        log.Printf("Received heartbeat from %s", msg.From)
        // Responder heartbeat
        gs.communicator.SendMessage(msg.From, "heartbeat_response", map[string]interface{}{
            "timestamp": time.Now(),
        })
    })
}

// Handlers HTTP
func (gs *GameServer) handleServerInfo(c *gin.Context) {
    info := gs.discovery.GetServerInfo()
    c.JSON(http.StatusOK, info)
}

func (gs *GameServer) handleGetPeers(c *gin.Context) {
    peers := gs.discovery.GetKnownPeers()
    c.JSON(http.StatusOK, gin.H{"peers": peers})
}

func (gs *GameServer) handleAddPeer(c *gin.Context) {
    var peer common.ServerInfo
    if err := c.ShouldBindJSON(&peer); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }
    
    gs.discovery.AddPeerManually(peer)
    c.JSON(http.StatusOK, gin.H{"status": "peer added"})
}

// Shutdown para graceful shutdown
func (gs *GameServer) Shutdown() error {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    if gs.httpServer != nil {
        return gs.httpServer.Shutdown(ctx)
    }
    return nil
}



func main() {
    // Carregar configuração
    config := common.LoadConfig()
    
    // Criar e inicializar servidor
    server := NewGameServer(config)
    
    if err := server.Start(); err != nil {
        log.Fatalf("Failed to start server: %v", err)
    }
    
    // Aguardar sinal de parada
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    
    log.Println("Shutting down server...")
    if err := server.Shutdown(); err != nil {
        log.Printf("Server shutdown error: %v", err)
    }
    log.Println("Server stopped")
}
