package API

import (
	"fmt"
	"math/rand"
	"sync"

	"github.com/google/uuid"
) 

type IdManager struct{
    Mutex sync.RWMutex
    Count int
    ClientMap map[int]*Player
}

type matchStruct struct {
    SelfId string `json:"self_id"`
    P1     int    `json:"p1"`
    P2     int    `json:"p2"`
    Card1  int    `json:"card1"`
    Card2  int    `json:"card2"`
}

type BattleQueue struct{
    Mutex sync.RWMutex
    ClientQueue []int
}

type TradeQueue struct{
    Mutex sync.RWMutex
    ClientQueue []int
}

type Player struct{
    Id int
    Cards []int
}

type PackStorage struct{
    Mutex sync.Mutex
    Cards [][3]int
}

/////////////////////////////////////////////////////////
var IM = IdManager{
	Count: 0,
	ClientMap: map[int]*Player{},
}

var BQ = BattleQueue{
    ClientQueue: make([]int, 0),
}

var TQ = TradeQueue{
    ClientQueue: make([]int, 0),
}

var STO = PackStorage{
	Cards : setupPacks(900),
}


func setupPacks(N int) [][3]int {
    arr := make([]int, N)
    for i := range N {
        arr[i] = i + 1
    }

	numPacks := (N + 3 - 1) / 3
	packs := make([][3]int, numPacks) 
	
	for i := range numPacks {
		start := i * 3
		end := start + 3
		
        sliceEnd := min(end, N)
        
        sourceSlice := arr[start:sliceEnd]
        copy(packs[i][:], sourceSlice)
	}

	return packs
}



func (s *Store) CreatePlayer() (int, error) {
	s.mu.Lock()
	
	s.count += 1
	newPlayer := Player{
		Id:    s.count,
		Cards: nil,
	}
	s.mu.Unlock()
	fmt.Println("DEBUG1")
	resp, err := s.applyLogInternal("create_player", "", "", "", "", &newPlayer, s.count, nil, []int{}, matchStruct{})
	fmt.Println("DEBUG2")
	if err != nil {
		return 0, err
	}

	// se applyLogInternal retornar o valor do FSM.Apply(), ótimo
	if resp != nil {
		if id, ok := resp.(int); ok {
			return id, nil
		}
	}

	return newPlayer.Id, nil
}

func (s *Store) OpenPack(id int) (*[3]int, error) {
	s.mu.Lock()
	
	i:=rand.Intn(len(s.Cards))
	fmt.Println(i)
	lastIndex := len(s.Cards) - 1
	
	pack := s.Cards[i]
	s.Cards[i] = s.Cards[lastIndex]
	s.Cards = s.Cards[:lastIndex]

	player := s.players[id]
	player.Cards = append(player.Cards, pack[0], pack[1], pack[2]) 
	s.players[id] = player
	
	s.mu.Unlock()
	fmt.Println("DEBUG1")
	resp, err := s.applyLogInternal("open_pack", "", "", "", "", &player, 0, &s.Cards, []int{}, matchStruct{})
	fmt.Println("DEBUG2")
	if err != nil {
		return nil, err
	}

	// se applyLogInternal retornar o valor do FSM.Apply(), ótimo
	if resp != nil {
		if id, ok := resp.([3]int); ok {
			return &id, nil
		}
	}

	return &pack, nil
}

func (s *Store) JoinQueue(id int) (int, error) {
	s.mu.Lock()
	
	player := Player{
		Id: id,
		Cards: nil,
	}

	s.mu.Unlock()
	fmt.Println("DEBUG1")
	resp, err := s.applyLogInternal("join_game_queue", "", "", "", "", &player, 0, nil, []int{}, matchStruct{})
	fmt.Println("DEBUG2")
	if err != nil {
		return 0, err
	}

	// se applyLogInternal retornar o valor do FSM.Apply(), ótimo
	if resp != nil {
		if id, ok := resp.(int); ok {
			return id, nil
		}
	}

	return player.Id, nil
}

func (s *Store) JoinTradeQueue(id int, card int) (int, error) {
	s.mu.Lock()
	
	player, ok := s.players[id]
    
	if !ok {
        return 0, fmt.Errorf("player with ID %d not found", id)
    }

    // 1. Find the index of the 'card' to remove
    indexToRemove := -1
    for i, c := range player.Cards {
        if c == card {
            indexToRemove = i
            break // Found the card, so we can stop searching
        }
    }

    if indexToRemove == -1 {
        // Handle case where the card is not in the player's hand
        return 0, fmt.Errorf("card %d not found for player %d", card, id)
    }

    player.Cards = append(player.Cards[:indexToRemove], player.Cards[indexToRemove+1:]...)

    s.players[id] = player

	s.mu.Unlock()
	fmt.Println("DEBUG1")
	resp, err := s.applyLogInternal("join_card_queue", "", "", "", "", &player, card, nil, []int{}, matchStruct{})
	fmt.Println("DEBUG2")
	if err != nil {
		return 0, err
	}

	// se applyLogInternal retornar o valor do FSM.Apply(), ótimo
	if resp != nil {
		if id, ok := resp.(int); ok {
			return id, nil
		}
	}

	return player.Id, nil
}

func (s *Store) CreateMatch() (matchStruct, error) {
	s.mu.Lock()
	
	p1:=s.gameQueue[0]
	p2:=s.gameQueue[1]
	gameId := uuid.New().String()
	x := matchStruct{
		P1: p1,P2: p2,SelfId: gameId,Card1: 0,Card2: 0,
	}
	fmt.Println(x)
	newQueue := s.gameQueue[2:]
	s.mu.Unlock()
	fmt.Println("DEBUG1")
	_, err := s.applyLogInternal("create_game", "", "", "", "", nil, 0, nil, newQueue, x)
	fmt.Println("DEBUG2")
	if err != nil {
		return matchStruct{}, err
	}

	// se applyLogInternal retornar o valor do FSM.Apply(), ótimo

	return x, nil
}

func (s *Store) CreateTrade() (int, error) {
	s.mu.Lock()
	
	p1:=s.tradeQueue[0]
	p2:=s.tradeQueue[1]
	
	newQueue := s.gameQueue[2:]
	s.mu.Unlock()
	fmt.Println("DEBUG1")
	_, err := s.applyLogInternal("create_trade", "", "", "", "", nil, 0, nil, newQueue, x)
	fmt.Println("DEBUG2")
	if err != nil {
		return matchStruct{}, err
	}

	// se applyLogInternal retornar o valor do FSM.Apply(), ótimo

	return x, nil
}

func (s *Store) PlayCard(gameId string, id int, card int) (error) {
	s.mu.Lock()
	
	game := s.matchHistory[gameId]
	if game.P1 == id {
		game.Card1 = card
	}else {
		game.Card2 = card
	}
	s.matchHistory[gameId] = game
	
	s.mu.Unlock()
	fmt.Println("DEBUG1")
	_, err := s.applyLogInternal("play_cards", "", "", "", "", nil, 0, nil, nil, game)
	fmt.Println("DEBUG2")
	if err != nil {
		return err
	}

	// se applyLogInternal retornar o valor do FSM.Apply(), ótimo

	return nil
}