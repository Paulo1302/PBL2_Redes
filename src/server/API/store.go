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
	Op         string `json:"op"`
	Key        string `json:"key"`
	Value      string `json:"value"`
	MemberID   string `json:"member_id"`
	MemberAddr string `json:"member_addr"`
}

// Store é a nossa FSM (Finite State Machine)
type Store struct {
	mu       sync.Mutex
	data     map[string]string
	members  map[string]raft.ServerAddress
	RaftLog  *raft.Raft
	NodeID   string
	RaftAddr string
}

// NewStore cria uma nova instância do nosso Store
func NewStore() *Store {
	return &Store{
		data:    make(map[string]string),
		members: make(map[string]raft.ServerAddress),
	}
}

// ----------------------------------------------------
// Implementação da interface raft.FSM
// ----------------------------------------------------

// Apply processa um comando replicado via Raft
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

// Snapshot cria um instantâneo do estado atual
func (s *Store) Snapshot() (raft.FSMSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cria cópias dos dados para evitar race conditions
	dataCopy := make(map[string]string)
	for k, v := range s.data {
		dataCopy[k] = v
	}

	membersCopy := make(map[string]raft.ServerAddress)
	for k, v := range s.members {
		membersCopy[k] = v
	}

	return &fsmSnapshot{
		data:    dataCopy,
		members: membersCopy,
	}, nil
}

// Restore restaura o estado a partir de um snapshot
func (s *Store) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	var snapData snapshotData
	if err := json.NewDecoder(rc).Decode(&snapData); err != nil {
		return err
	}

	s.mu.Lock()
	s.data = snapData.Data
	s.members = snapData.Members
	s.mu.Unlock()

	return nil
}

// GetMembers retorna cópia segura da lista de membros
func (s *Store) GetMembers() map[string]raft.ServerAddress {
	s.mu.Lock()
	defer s.mu.Unlock()

	membersCopy := make(map[string]raft.ServerAddress)
	for k, v := range s.members {
		membersCopy[k] = v
	}
	return membersCopy
}

// ----------------------------------------------------
// Estruturas auxiliares para Snapshot
// ----------------------------------------------------

// snapshotData é a estrutura serializada do snapshot
type snapshotData struct {
	Data    map[string]string                  `json:"data"`
	Members map[string]raft.ServerAddress `json:"members"`
}

// fsmSnapshot implementa raft.FSMSnapshot
type fsmSnapshot struct {
	data    map[string]string
	members map[string]raft.ServerAddress
}

// ----------------------------------------------------
// Implementação da interface raft.FSMSnapshot
// ----------------------------------------------------

// Persist salva o snapshot no sink fornecido
func (f *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	snapData := snapshotData{
		Data:    f.data,
		Members: f.members,
	}

	err := json.NewEncoder(sink).Encode(snapData)
	if err != nil {
		sink.Cancel() // CORREÇÃO: Cancel() não retorna error
		return fmt.Errorf("failed to encode snapshot: %w", err)
	}

	return sink.Close()
}

// Release libera recursos do snapshot
func (f *fsmSnapshot) Release() {
	f.data = nil
	f.members = nil
}