package main

import (
	"slices"
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"client/API"

	"github.com/nats-io/nats.go"
)

var conn *nats.Conn

func userMenu(nc *nats.Conn) {
	id := 0
	cardChan := make(chan int)
	gameResult := make(chan string)
	pubsub.ManageGame2(nc, &id, cardChan, gameResult)
	reader := bufio.NewReader(os.Stdin)

	for {
		id = menuInicial(nc, reader)
		if id != 0 {
			sub := pubsub.LoggedIn(nc, id)
			menuPrincipal(nc, id, reader, cardChan, gameResult)
			sub.Unsubscribe()
		}
	}
}

func menuInicial(nc *nats.Conn, reader *bufio.Reader) int {
	for {
		fmt.Println("1 - Ping")
		fmt.Println("2 - Login")
		fmt.Println("3 - Criar usuário")
		fmt.Print("> ")

		opt, _ := reader.ReadString('\n')
		opt = strings.TrimSpace(opt)

		switch opt {
		case "1":
			ping := pubsub.RequestPing(nc)
			if ping == -1 {
				fmt.Println("Erro no ping")
			} else {
				fmt.Println("Ping:", ping, "ms")
			}
		case "2":
			fmt.Print("ID: ")
			text, _ := reader.ReadString('\n')
			text = strings.TrimSpace(text)
			id, _ := strconv.Atoi(text)
			if id <= 0 {
				continue
			}
			alreadyLogged, err := pubsub.RequestLogin(nc, id)
			if err != nil || alreadyLogged{
				fmt.Println("Erro no login")
			} else {
				fmt.Println("Login bem-sucedido!")
				return id
			}
		case "3":
			userID := pubsub.RequestCreateAccount(nc)
			if userID == 0 {
				fmt.Println("Erro ao criar usuário")
			} else {
				fmt.Println("Usuário criado! ID:", userID)
				return userID
			}
		default:
			fmt.Println("Opção inválida")
		}
	}
}

func menuPrincipal(nc *nats.Conn, id int, reader *bufio.Reader, card chan(int), roundResult chan(string)) {
	var cards []int
	var err error


	for {
		fmt.Println("1 - Abrir pacote")
		fmt.Println("2 - Ver cartas")
		fmt.Println("3 - Trocar carta")
		fmt.Println("4 - Encontrar partida")
		fmt.Println("5 - Sair")
		fmt.Print("> ")

		opt, _ := reader.ReadString('\n')
		opt = strings.TrimSpace(opt)

		switch opt {
		case "1":
			cards, err = pubsub.RequestOpenPack(nc, id)
			if err != nil {
				fmt.Println("Erro:", err)
			} else {
				fmt.Println("Cartas obtidas:", cards)
			}
		case "2":
			cards, err = pubsub.RequestSeeCards(nc, id)
			if err != nil {
				fmt.Println("Erro:", err)
			} else {
				fmt.Println("Suas cartas:", cards)
			}
		case "3":
			fmt.Print("Carta para trocar: ")
			text, _ := reader.ReadString('\n')
			text = strings.TrimSpace(text)
			num, _ := strconv.Atoi(text)
			newCard, err := pubsub.RequestTradeCards(nc, id, num)
			if err != nil {
				fmt.Println("Erro:", err)
			} else {
				fmt.Println("Nova carta:", newCard)
			}
		case "4":
			fmt.Println("Buscando partida...")
			cards, _ = pubsub.RequestSeeCards(nc, id)
			if len(cards)==0 {
				fmt.Println("não tem cartas")
				continue
			}
			game, err := pubsub.RequestFindMatch(nc, id)
			if err != nil {
				fmt.Println("Erro:", err)
			} else {
				menuJogo(nc, id, cards, reader, card, roundResult, game)
			}
		case "5":
			return
		default:
			fmt.Println("Opção inválida")
		}
	}
}

func menuJogo(nc *nats.Conn, id int, cards []int, reader *bufio.Reader, cardChan chan(int), gameResult chan(string), game string) {
	fmt.Println("Suas cartas:", cards)
	s:=pubsub.ImAlive(conn, id)
	for {
		fmt.Print("Carta para jogar: ")
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)
		num, _ := strconv.Atoi(text)

		valid := slices.Contains(cards, num)

		if !valid {
			fmt.Println("Carta inválida")
			continue
		}

		pubsub.SendCards(nc, id, num, game)
		break
	}

	fmt.Println("O oponente jogou: ", <-cardChan)
	fmt.Println("O resultado do jogo foi: ", <-gameResult)
	s.Unsubscribe()
}

func main() {

	var htb = time.Now().UnixMilli()
	
	nc := pubsub.BrokerConnect(0)
	conn = nc
	defer nc.Close()

	go pubsub.Heartbeat(nc, &htb)
	go userMenu(nc)

	for time.Now().UnixMilli() - htb < 1000{};
	
}