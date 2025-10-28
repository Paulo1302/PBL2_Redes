package API

import (
	"fmt"
	"math/rand"
	"sync"
) 

type IdManager struct{
    Mutex sync.RWMutex
    Count int
    ClientMap map[int]*Player
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

    for i := N - 1; i > 0; i-- {
        j := rand.Intn(i + 1)
        arr[i], arr[j] = arr[j], arr[i]
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

func SetupGameState(){
	return
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
	resp, err := s.applyLogInternal("create_player", "", "", "", "", &newPlayer, s.count)
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

// func (s *Store) OpenPack() (int, error) {
// 	s.mu.Lock()
	
// 	s.count += 1
// 	pack := s.Cards[1]
// 	s.Cards = s.Cards[1:]
// 	s.mu.Unlock()
// 	fmt.Println("DEBUG1")
// 	resp, err := s.applyLogInternal("open_pack", "", "", "", "", nil, 0, &pack)
// 	fmt.Println("DEBUG2")
// 	if err != nil {
// 		return 0, err
// 	}

// 	// se applyLogInternal retornar o valor do FSM.Apply(), ótimo
// 	if resp != nil {
// 		if id, ok := resp.(int); ok {
// 			return id, nil
// 		}
// 	}

// 	return newPlayer.Id, nil
// }
