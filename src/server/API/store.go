package API

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/hashicorp/raft"
)

// Define a estrutura para comandos Raft (Logs)
type command struct {
	Op    string `json:"op"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Store é a nossa FSM (Finite State Machine)
// Contém os campos NodeID e RaftAddr para contornar problemas de versão/acesso
type Store struct {
	mu       sync.Mutex 
	data     map[string]string
	RaftLog  *raft.Raft  // <--- CORREÇÃO: Agora é público
	NodeID   string      
	RaftAddr string      
}

// NewStore cria uma nova instância do nosso Store
func NewStore() *Store {
	return &Store{
		data: make(map[string]string),
	}
}

// ----------------------------------------------------
// Implementação da interface raft.FSM
// ----------------------------------------------------

// Apply é chamado quando um log Raft é committed (aplicado).
func (s *Store) Apply(log *raft.Log) interface{} {
	var c command
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
	default:
		return fmt.Errorf("unrecognized command op: %s", c.Op)
	}
}

// Snapshot é chamado para criar um instantâneo do estado atual.
func (s *Store) Snapshot() (raft.FSMSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	o := make(map[string]string)
	for k, v := range s.data {
		o[k] = v
	}
	return &snapshot{store: o}, nil
}

// Restore é chamado para restaurar o estado a partir de um snapshot.
func (s *Store) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	decoder := json.NewDecoder(rc)
	var o map[string]string
	if err := decoder.Decode(&o); err != nil {
		return err
	}

	s.mu.Lock()
	s.data = o
	s.mu.Unlock()
	return nil
}

// ----------------------------------------------------
// Implementação da interface raft.FSMSnapshot
// ----------------------------------------------------

type snapshot struct {
	store map[string]string
}

func (s *snapshot) Persist(sink raft.SnapshotSink) error {
	defer sink.Close()
	err := json.NewEncoder(sink).Encode(s.store)
	if err != nil {
		return sink.Cancel()
	}
	return nil
}

func (s *snapshot) Release() {
    s.store = nil 
}