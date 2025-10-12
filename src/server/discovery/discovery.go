package discovery

import (
    "fmt"
    "log"
    "net"
    "sync"
    "time"
    
    "cardGame/common"
)

// ManualDiscovery implementa descoberta manual simples
type ManualDiscovery struct {
    serverID      string
    serverInfo    common.ServerInfo
    knownPeers    map[string]common.ServerInfo
    callbacks     []func(common.ServerInfo, bool)
    mutex         sync.RWMutex
}

// NewManualDiscovery cria nova instância de descoberta manual
func NewManualDiscovery(serverID string, config common.ServerConfig) *ManualDiscovery {
    // Detectar IP local
    localIP := getLocalIP()
    
    serverInfo := common.ServerInfo{
        ServerID:    serverID,
        Address:     localIP,
        HTTPPort:    config.HTTPPort,
        RaftPort:    config.RaftPort,
        NATSPort:    config.NATSPort,
        Status:      "active",
        LastSeen:    time.Now(),
        IsBootstrap: config.IsBootstrap,
        Version:     config.Version,
    }
    
    md := &ManualDiscovery{
        serverID:   serverID,
        serverInfo: serverInfo,
        knownPeers: make(map[string]common.ServerInfo),
    }
    
    // Adicionar peers conhecidos da configuração
    for _, peerAddr := range config.KnownPeers {
        if peerAddr != "" {
            md.addPeerFromAddress(peerAddr)
        }
    }
    
    return md
}

// addPeerFromAddress adiciona peer a partir de endereço
func (md *ManualDiscovery) addPeerFromAddress(address string) {
    // Parse simples: assume formato ip:port ou apenas ip
    host := address
    if host == "" {
        return
    }
    
    peerID := fmt.Sprintf("peer-%s", host)
    peer := common.ServerInfo{
        ServerID:    peerID,
        Address:     host,
        HTTPPort:    8080, // Assume porta padrão
        RaftPort:    7000, // Assume porta padrão
        NATSPort:    4222, // Assume porta padrão
        Status:      "unknown",
        LastSeen:    time.Now(),
        IsBootstrap: false,
        Version:     "unknown",
    }
    
    md.mutex.Lock()
    md.knownPeers[peerID] = peer
    md.mutex.Unlock()
    
    log.Printf("Added peer from config: %s (%s)", peerID, host)
    
    // Notificar callbacks
    for _, callback := range md.callbacks {
        go callback(peer, true)
    }
}

// StartDiscovery inicia processo de descoberta
func (md *ManualDiscovery) StartDiscovery() error {
    log.Printf("Starting manual discovery for server %s", md.serverID)
    log.Printf("Known peers: %d", len(md.knownPeers))
    return nil
}

// RegisterCallback registra callback para novos peers
func (md *ManualDiscovery) RegisterCallback(callback func(common.ServerInfo, bool)) {
    md.callbacks = append(md.callbacks, callback)
}

// GetServerInfo retorna informações do servidor local
func (md *ManualDiscovery) GetServerInfo() common.ServerInfo {
    return md.serverInfo
}

// GetKnownPeers retorna peers conhecidos
func (md *ManualDiscovery) GetKnownPeers() []common.ServerInfo {
    md.mutex.RLock()
    defer md.mutex.RUnlock()
    
    peers := make([]common.ServerInfo, 0, len(md.knownPeers))
    for _, peer := range md.knownPeers {
        peers = append(peers, peer)
    }
    return peers
}

// AddPeerManually adiciona peer manualmente
func (md *ManualDiscovery) AddPeerManually(peer common.ServerInfo) error {
    md.mutex.Lock()
    md.knownPeers[peer.ServerID] = peer
    md.mutex.Unlock()
    
    log.Printf("Added peer manually: %s (%s:%d)", peer.ServerID, peer.Address, peer.HTTPPort)
    
    for _, callback := range md.callbacks {
        go callback(peer, true)
    }
    
    return nil
}

// getLocalIP detecta IP local
func getLocalIP() string {
    conn, err := net.Dial("udp", "8.8.8.8:80")
    if err != nil {
        log.Printf("Could not detect local IP, using localhost: %v", err)
        return "localhost"
    }
    defer conn.Close()
    
    localAddr := conn.LocalAddr().(*net.UDPAddr)
    return localAddr.IP.String()
}
