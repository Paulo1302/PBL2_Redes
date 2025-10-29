package API

import (
	"maps"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/hashicorp/raft"
)

// command define a estrutura para comandos Raft (Logs)
type command struct {
	Op         string        `json:"op"`
	Key        string        `json:"key"`
	Value      string        `json:"value"`
	MemberID   string        `json:"member_id"`
	MemberAddr string        `json:"member_addr"`
	Count      int           `json:"count"`
	GameId     matchStruct   `json:"game_id,omitempty"`
	PlayerID   int           `json:"player_id,omitempty"`
	PlayerCards []int        `json:"player_cards,omitempty"`
	Cards      [][3]int      `json:"cards,omitempty"`
	GameQueue  []int         `json:"game_queue,omitempty"`
}

// Store é a nossa FSM (Finite State Machine)
// Contém os dados replicados e a lista de membros do cluster.
type Store struct {
	mu           sync.Mutex
	data         map[string]string             // Dados chave-valor replicados
	members      map[string]raft.ServerAddress // Lista sincronizada de membros do cluster
	players      map[int]Player
	matchHistory map[string]matchStruct
	gameQueue    []int
	Cards        [][3]int
	count        int
	RaftLog      *raft.Raft // Referência à instância Raft (preenchida pelo main)
	NodeID       string     // ID deste nó (preenchido pelo main)
	RaftAddr     string     // Endereço Raft deste nó (preenchido pelo main)
}

// NewStore cria uma nova instância inicializada do Store (FSM).
func NewStore() *Store {
	return &Store{
		data:         make(map[string]string),
		members:      make(map[string]raft.ServerAddress),
		players:      make(map[int]Player),
		matchHistory: make(map[string]matchStruct),
		gameQueue:    make([]int, 0),
		count:        0,
		Cards:        setupPacks(900),
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
		fmt.Printf("[FSM Apply] Set key '%s' to '%s'\n", c.Key, c.Value)
		return nil
	case "delete":
		delete(s.data, c.Key)
		fmt.Printf("[FSM Apply] Deleted key '%s'\n", c.Key)
		return nil
	case "add_member":
		s.members[c.MemberID] = raft.ServerAddress(c.MemberAddr)
		fmt.Printf("[FSM Apply] Added member '%s' at '%s'\n", c.MemberID, c.MemberAddr)
		return nil
	case "remove_member":
		delete(s.members, c.MemberID)
		fmt.Printf("[FSM Apply] Removed member '%s'\n", c.MemberID)
		return nil
	case "create_player":
		s.count = c.Count // mantém o contador consistente
		s.players[c.PlayerID] = Player{
			Id:    c.PlayerID,
			Cards: c.PlayerCards,
		}
		fmt.Printf("[FSM Apply] Created player %d (count=%d)\n", c.PlayerID, s.count)
		return c.PlayerID
	case "open_pack":
		// atualiza player cards e o deck global (Cards)
		s.players[c.PlayerID] = Player{
			Id:    c.PlayerID,
			Cards: c.PlayerCards,
		}
		if c.Cards != nil {
			s.Cards = c.Cards
		}
		fmt.Println("[FSM Apply] Pack open", c.PlayerID, c.PlayerCards, "cards=", c.Cards)
		return nil
	case "join_game_queue":
		s.gameQueue = append(s.gameQueue, c.PlayerID)
		fmt.Println("[FSM Apply] join game queue", c.PlayerID, "queue=", s.gameQueue)
		return nil
	case "create_game":
		// assume GameId.selfId existe em matchStruct
		s.matchHistory[c.GameId.SelfId] = c.GameId
		s.gameQueue = c.GameQueue
		fmt.Println("[FSM Apply] created game", "queue=", c.GameQueue, c.GameId)
		return nil
	case "play_cards":
		s.matchHistory[c.GameId.SelfId] = c.GameId
		fmt.Println("[FSM Apply] card played", "game=", c.GameId)
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

	// Cópias seguras dos mapas
	dataCopy := make(map[string]string, len(s.data))
	maps.Copy(dataCopy, s.data)

	membersCopy := make(map[string]raft.ServerAddress, len(s.members))
	maps.Copy(membersCopy, s.members)

	playersCopy := make(map[int]Player, len(s.players))
	maps.Copy(playersCopy, s.players)

	matchCopy := make(map[string]matchStruct, len(s.matchHistory))
	maps.Copy(matchCopy, s.matchHistory)

	// cópia do gameQueue
	queueCopy := make([]int, len(s.gameQueue))
	copy(queueCopy, s.gameQueue)

	// cópia das cards
	cardsCopy := make([][3]int, len(s.Cards))
	copy(cardsCopy, s.Cards)

	fmt.Println("[FSM Snapshot] Creating snapshot") // Log de depuração
	return &fsmSnapshot{
		data:         dataCopy,
		members:      membersCopy,
		players:      playersCopy,
		matchHistory: matchCopy,
		Cards:        cardsCopy,
		gameQueue:    queueCopy,
		count:        s.count,
	}, nil
}

// Restore restaura o estado da FSM a partir de um snapshot recebido.
// Chamado quando um nó está a inicializar ou muito atrasado.
func (s *Store) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	decoder := json.NewDecoder(rc)

	// Define a estrutura esperada do snapshot (deve corresponder a fsmSnapshot.Persist)
	var snapshotData struct {
		Data         map[string]string             `json:"data"`
		Members      map[string]raft.ServerAddress `json:"members"`
		Players      map[int]Player                `json:"players"`
		MatchHistory map[string]matchStruct        `json:"match_history"`
		Cards        [][3]int                      `json:"cards"`
		GameQueue    []int                         `json:"game_queue"`
		Count        int                           `json:"count"`
	}

	// Decodifica o snapshot
	if err := decoder.Decode(&snapshotData); err != nil {
		return fmt.Errorf("failed to decode snapshot: %w", err)
	}

	// Aplica o estado do snapshot à FSM
	s.mu.Lock()
	s.data = snapshotData.Data
	s.members = snapshotData.Members
	s.players = snapshotData.Players
	s.matchHistory = snapshotData.MatchHistory
	s.Cards = snapshotData.Cards
	s.gameQueue = snapshotData.GameQueue
	s.count = snapshotData.Count
	s.mu.Unlock()

	fmt.Printf("[FSM Restore] Restored state from snapshot: players=%d, cards=%d, count=%d, queue=%d\n",
		len(s.players), len(s.Cards), s.count, len(s.gameQueue))
	return nil
}

// GetMembers retorna uma cópia segura (deep copy) da lista atual de membros.
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
	data         map[string]string
	members      map[string]raft.ServerAddress
	players      map[int]Player
	matchHistory map[string]matchStruct
	Cards        [][3]int
	gameQueue    []int
	count        int
}

// Persist salva o conteúdo do snapshot (os mapas) no sink fornecido pelo Raft.
// O Raft geralmente fornece um arquivo temporário como sink.
func (f *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	err := func() error {
		payload := struct {
			Data         map[string]string             `json:"data"`
			Members      map[string]raft.ServerAddress `json:"members"`
			Players      map[int]Player                `json:"players"`
			MatchHistory map[string]matchStruct        `json:"match_history"`
			Cards        [][3]int                      `json:"cards"`
			GameQueue    []int                         `json:"game_queue"`
			Count        int                           `json:"count"`
		}{
			Data:         f.data,
			Members:      f.members,
			Players:      f.players,
			MatchHistory: f.matchHistory,
			Cards:        f.Cards,
			GameQueue:    f.gameQueue,
			Count:        f.count,
		}

		encoder := json.NewEncoder(sink)
		if err := encoder.Encode(&payload); err != nil {
			return fmt.Errorf("failed to encode snapshot payload: %w", err)
		}
		if err := sink.Close(); err != nil {
			return fmt.Errorf("failed to close snapshot sink: %w", err)
		}
		return nil
	}()

	if err != nil {
		_ = sink.Cancel()
		fmt.Printf("[FSM Snapshot Persist] Error: %v\n", err)
		return err
	}

	fmt.Println("[FSM Snapshot Persist] Snapshot persisted successfully")
	return nil
}

// Release é chamado pelo Raft quando o snapshot não é mais necessário.
// Libera a memória usada pelo snapshot.
func (f *fsmSnapshot) Release() {
	f.data = nil
	f.members = nil
	f.players = nil
	f.matchHistory = nil
	f.Cards = nil
	f.gameQueue = nil
	fmt.Println("[FSM Snapshot Release] Snapshot resources released")
}
