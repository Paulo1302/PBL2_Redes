package API

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/hashicorp/raft"
)

// command define a estrutura para comandos Raft (Logs)
type command struct {
	Op         string `json:"op"`
	Key        string `json:"key"`
	Value      string `json:"value"`
	MemberID   string `json:"member_id"`
	MemberAddr string `json:"member_addr"`
	Count      int
	PlayerID   int    `json:"player_id,omitempty"`
    Cards      []int  `json:"cards,omitempty"`
}

// Store é a nossa FSM (Finite State Machine)
// Contém os dados replicados e a lista de membros do cluster.
type Store struct {
	mu       sync.Mutex
	data     map[string]string             // Dados chave-valor replicados
	members  map[string]raft.ServerAddress // Lista sincronizada de membros do cluster
	players  map[int]Player
	Cards    [][3]int
	count    int 
	RaftLog  *raft.Raft                    // Referência à instância Raft (preenchida pelo main)
	NodeID   string                        // ID deste nó (preenchido pelo main)
	RaftAddr string                        // Endereço Raft deste nó (preenchido pelo main)
}

// NewStore cria uma nova instância inicializada do Store (FSM).
func NewStore() *Store {
	return &Store{
		data:    make(map[string]string),
		members: make(map[string]raft.ServerAddress),
		players: make(map[int]Player),
		count: 0,
		Cards: setupPacks(900),
	}
}

// ----------------------------------------------------
// Implementação da interface raft.FSM
// ----------------------------------------------------

// Apply processa um comando (log) que foi cometido pelo cluster Raft.
// Esta função é chamada em *todos* os nós para garantir a consistência.
func (s *Store) Apply(log *raft.Log) interface{} {
	var c command
	if err := json.Unmarshal(log.Data, &c); err != nil {
		// Retorna um erro que será registrado pelo Raft
		return fmt.Errorf("failed to unmarshal command: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Aplica a operação com base no tipo de comando
	switch c.Op {
	case "set":
		s.data[c.Key] = c.Value
		fmt.Printf("[FSM Apply] Set key '%s' to '%s'\n", c.Key, c.Value) // Log de depuração
		return nil // Sucesso
	case "delete":
		delete(s.data, c.Key)
		fmt.Printf("[FSM Apply] Deleted key '%s'\n", c.Key) // Log de depuração
		return nil // Sucesso
	case "add_member":
		s.members[c.MemberID] = raft.ServerAddress(c.MemberAddr)
		fmt.Printf("[FSM Apply] Added member '%s' at '%s'\n", c.MemberID, c.MemberAddr) // Log de depuração
		return nil // Sucesso
	case "remove_member":
		delete(s.members, c.MemberID)
		fmt.Printf("[FSM Apply] Removed member '%s'\n", c.MemberID) // Log de depuração
		return nil // Sucesso
	case "create_player":
		s.count = c.Count // mantém o contador consistente
		s.players[c.PlayerID] = Player{
			Id:    c.PlayerID,
			Cards: c.Cards,
		}
		fmt.Printf("[FSM Apply] Created player %d (count=%d)\n", c.PlayerID, s.count)
		return nil
	default:
		// Retorna um erro se o comando for desconhecido
		return fmt.Errorf("unrecognized command op: %s", c.Op)
	}
}

// Snapshot cria um instantâneo consistente do estado atual da FSM.
// Chamado periodicamente pelo Raft para truncar o log.
func (s *Store) Snapshot() (raft.FSMSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cria cópias profundas dos mapas para garantir a consistência do snapshot
	dataCopy := make(map[string]string, len(s.data))
	for k, v := range s.data {
		dataCopy[k] = v
	}
	membersCopy := make(map[string]raft.ServerAddress, len(s.members))
	for k, v := range s.members {
		membersCopy[k] = v
	}

	fmt.Println("[FSM Snapshot] Creating snapshot") // Log de depuração
	return &fsmSnapshot{
		data:    dataCopy,
		members: membersCopy,
	}, nil // Sucesso
}

// Restore restaura o estado da FSM a partir de um snapshot recebido.
// Chamado quando um nó está a inicializar ou muito atrasado.
func (s *Store) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	decoder := json.NewDecoder(rc)

	// Define a estrutura esperada do snapshot (deve corresponder a fsmSnapshot.Persist)
	var snapshotData struct {
		Data    map[string]string             `json:"data"`
		Members map[string]raft.ServerAddress `json:"members"`
	}

	// Decodifica o snapshot
	if err := decoder.Decode(&snapshotData); err != nil {
		return fmt.Errorf("failed to decode snapshot: %w", err)
	}

	// Aplica o estado do snapshot à FSM
	s.mu.Lock()
	s.data = snapshotData.Data
	s.members = snapshotData.Members
	s.mu.Unlock()

	fmt.Println("[FSM Restore] Restored state from snapshot") // Log de depuração
	return nil // Sucesso
}

// GetMembers retorna uma cópia segura (deep copy) da lista atual de membros.
// Útil para endpoints de API que precisam mostrar os membros.
func (s *Store) GetMembers() map[string]raft.ServerAddress {
	s.mu.Lock()
	defer s.mu.Unlock()

	membersCopy := make(map[string]raft.ServerAddress, len(s.members))
	for k, v := range s.members {
		membersCopy[k] = v
	}
	return membersCopy
}

// ----------------------------------------------------
// Implementação da interface raft.FSMSnapshot
// ----------------------------------------------------

// fsmSnapshot representa um snapshot persistente do estado do Store.
type fsmSnapshot struct {
	data    map[string]string
	members map[string]raft.ServerAddress
}

// Persist salva o conteúdo do snapshot (os mapas) no sink fornecido pelo Raft.
// O Raft geralmente fornece um arquivo temporário como sink.
func (f *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	// Função para garantir que o sink seja fechado ou cancelado em caso de erro
	err := func() error {
		// Estrutura para serializar (deve corresponder a Restore)
		payload := struct {
			Data    map[string]string             `json:"data"`
			Members map[string]raft.ServerAddress `json:"members"`
		}{
			Data:    f.data,
			Members: f.members,
		}

		// Codifica os dados como JSON e escreve no sink
		encoder := json.NewEncoder(sink)
		if err := encoder.Encode(&payload); err != nil {
			return fmt.Errorf("failed to encode snapshot payload: %w", err)
		}

		// Fecha o sink para indicar que a escrita está completa
		if err := sink.Close(); err != nil {
			return fmt.Errorf("failed to close snapshot sink: %w", err)
		}
		return nil // Sucesso
	}()

	// Se ocorreu algum erro durante a persistência, cancela o snapshot
	if err != nil {
		_ = sink.Cancel() // Ignora erro no Cancel, pois já temos o erro original
		fmt.Printf("[FSM Snapshot Persist] Error: %v\n", err) // Log de depuração
		return err
	}

	fmt.Println("[FSM Snapshot Persist] Snapshot persisted successfully") // Log de depuração
	return nil // Sucesso
}

// Release é chamado pelo Raft quando o snapshot não é mais necessário.
// Libera a memória usada pelo snapshot.
func (f *fsmSnapshot) Release() {
	f.data = nil    // Permite que o GC colete o mapa antigo
	f.members = nil // Permite que o GC colete o mapa antigo
	fmt.Println("[FSM Snapshot Release] Snapshot resources released") // Log de depuração
}
