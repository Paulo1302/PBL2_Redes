package common

import (
    "fmt"
    "os"
    "strconv"
    "strings"
)

// ServerConfig contém configuração completa do servidor
type ServerConfig struct {
    ServerID     string
    HTTPPort     int
    RaftPort     int
    NATSPort     int
    IsBootstrap  bool
    KnownPeers   []string
    ClusterName  string
    DataDir      string
    Version      string
}

// LoadConfig carrega configuração com valores padrão compatíveis
func LoadConfig() ServerConfig {
    return ServerConfig{
        ServerID:    getEnvWithDefault("SERVER_ID", generateServerID()),
        HTTPPort:    getEnvInt("HTTP_PORT", 8080),
        RaftPort:    getEnvInt("RAFT_PORT", 7000),
        NATSPort:    getEnvInt("NATS_PORT", 4222),
        IsBootstrap: getEnvBool("IS_BOOTSTRAP", false),
        KnownPeers:  getEnvStringSlice("KNOWN_PEERS", []string{}),
        ClusterName: getEnvWithDefault("CLUSTER_NAME", "card-game-cluster"),
        DataDir:     getEnvWithDefault("DATA_DIR", "./data"),
        Version:     getEnvWithDefault("VERSION", "1.0.0"),
    }
}

func getEnvWithDefault(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
    if value := os.Getenv(key); value != "" {
        if intVal, err := strconv.Atoi(value); err == nil {
            return intVal
        }
    }
    return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
    if value := os.Getenv(key); value != "" {
        if boolVal, err := strconv.ParseBool(value); err == nil {
            return boolVal
        }
    }
    return defaultValue
}

func getEnvStringSlice(key string, defaultValue []string) []string {
    if value := os.Getenv(key); value != "" && strings.TrimSpace(value) != "" {
        return strings.Split(strings.TrimSpace(value), ",")
    }
    return defaultValue
}

func generateServerID() string {
    hostname, _ := os.Hostname()
    return fmt.Sprintf("server-%s", hostname)
}
