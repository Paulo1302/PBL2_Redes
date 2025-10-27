package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"client/API"

	"github.com/nats-io/nats.go"
)


func userMenu(nc *nats.Conn) {
	reader := bufio.NewReader(os.Stdin)

	for {
		id := menuInicial(nc, reader)
		if id != 0 {
			sub := pubsub.LoggedIn(nc, id)
			menuPrincipal(nc, id, reader)
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
			userID, err := pubsub.RequestLogin(nc, id)
			if err != nil {
				fmt.Println("Erro no login:", err)
			} else {
				fmt.Println("Login bem-sucedido! ID:", userID)
				return userID
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

func menuPrincipal(nc *nats.Conn, id int, reader *bufio.Reader) {
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
			_, err := pubsub.RequestFindMatch(nc, id)
			if err != nil {
				fmt.Println("Erro:", err)
			} else {
				cards, _ = pubsub.RequestSeeCards(nc, id)
				menuJogo(nc, id, cards, reader)
			}
		case "5":
			return
		default:
			fmt.Println("Opção inválida")
		}
	}
}

func menuJogo(nc *nats.Conn, id int, cards []int, reader *bufio.Reader) {
	fmt.Println("Suas cartas:", cards)

	cardChan := make(chan int)
	gameResult := make(chan string)
	ready := make(chan struct{})

	go pubsub.ManageGame(nc, id, cardChan, ready, gameResult)

	<-ready

	for {
		fmt.Print("Carta para jogar: ")
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)
		num, _ := strconv.Atoi(text)

		valid := false
		for _, c := range cards {
			if c == num {
				valid = true
				break
			}
		}

		if !valid {
			fmt.Println("Carta inválida")
			continue
		}

		pubsub.SendCards(nc, id, num)
		break
	}

	fmt.Println("O oponente jogou: ", <-cardChan)
	fmt.Println("O resultado do jogo foi: ", <-gameResult)
}

func main() {

	var htb = time.Now().UnixMilli()
	
	nc := pubsub.BrokerConnect(0)
	defer nc.Close()

	go pubsub.Heartbeat(nc, &htb)
	go userMenu(nc)

	for time.Now().UnixMilli() - htb < 1000{};
	
}