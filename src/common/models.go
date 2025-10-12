package common

import "time"

// ServerInfo contém informações de um servidor
type ServerInfo struct {
    ServerID    string    `json:"server_id"`
    Address     string    `json:"address"`
    HTTPPort    int       `json:"http_port"`
    RaftPort    int       `json:"raft_port"`
    NATSPort    int       `json:"nats_port"`
    Status      string    `json:"status"`
    LastSeen    time.Time `json:"last_seen"`
    IsBootstrap bool      `json:"is_bootstrap"`
    Version     string    `json:"version"`
}

// ServerCommunicationMessage representa mensagens entre servidores
type ServerCommunicationMessage struct {
    Type      string      `json:"type"`
    From      string      `json:"from"`
    To        string      `json:"to"`
    Data      interface{} `json:"data"`
    Timestamp time.Time   `json:"timestamp"`
    MessageID string      `json:"message_id"`
}

// HealthStatus representa status de saúde do servidor
type HealthStatus struct {
    ServerID    string    `json:"server_id"`
    Status      string    `json:"status"`
    Timestamp   time.Time `json:"timestamp"`
    PeerCount   int       `json:"peer_count"`
    LastPing    int64     `json:"last_ping_ms"`
}
