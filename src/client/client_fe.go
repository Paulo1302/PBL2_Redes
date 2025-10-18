package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"
	

	"client/API" // Assuming your provided pubsub code is in client/pubsub
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nats-io/nats.go"
)
type (
	StatusMsg struct {
		isError bool
		message string
	}
	LoginMsg struct {
		clientID int
	}
	CardListMsg struct {
		cards []int
	}
	EnemyFoundMsg struct {
		enemyID int
		cards   []int // A coleção de cartas do usuário
	}
	TradeResultMsg struct {
		newCard int
	}
	GameUpdateMsg struct {
		result string
		card   int
	}
)

// --- Global State & Constants ---

const (
	// Menu State
	authMenu = iota // Login ou Create Account
	mainMenu        // Menu principal de ações
	loginForm       // Estado de formulário para digitar o Client ID
	tradeForm       // Estado de formulário para digitar a carta para troca
	gameMenu        // Estado de jogo: SendCard
)

const (
	natsServerNumber = 0
	timeout          = time.Second * 3
)

// --- Keybindings ---

type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Enter    key.Binding
	Back     key.Binding
	Quit     key.Binding
	Help     key.Binding
	Ping     key.Binding
	OpenPack key.Binding
	SeeCards key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.Back},
		{k.Help, k.Quit, k.Ping, k.OpenPack, k.SeeCards},
	}
}

var keys = keyMap{
	Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "mover p/ cima")),
	Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "mover p/ baixo")),
	Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "selecionar/confirmar")),
	Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "voltar/cancelar")),
	Quit:     key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "sair")),
	Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "ajuda")),
	Ping:     key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "ping")),
	OpenPack: key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "abrir pack")),
	SeeCards: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "ver cartas")),
}

// --- Item/Delegate for List ---

type item struct {
	title, desc string
	action      int
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

// --- Model Definition ---

type model struct {
	// Core State
	clientID  int
	currentState int
	natsConn  *nats.Conn
	heartbeat *int64
	help      help.Model
	statusMsg StatusMsg
	cards     []int // A coleção de cartas do usuário

	// Menus/Lists
	authList list.Model
	mainList list.Model
	gameList list.Model

	// Form Inputs
	textInput textinput.Model // Componente usado para login e trade
	
	// Game State
	enemyID       int
	gameCardInput chan int // Comunicação entre TUI e ManageGame
	gameCancel    context.CancelFunc
}

func initialModel(nc *nats.Conn, heartbeat *int64) model {
	m := model{
		natsConn:  nc,
		heartbeat: heartbeat,
		help:      help.New(),
		currentState: authMenu,
		statusMsg: StatusMsg{message: "Bem-vindo! Faça Login ou Crie uma conta."},
		textInput: textinput.New(), // Inicializa o text input
	}

	m.textInput.Placeholder = "Digite o Client ID..."
	m.textInput.Focus()
	m.textInput.CharLimit = 10
	m.textInput.Width = 20
	m.textInput.Prompt = "ID: "

	// --- Auth Menu Setup ---
	authItems := []list.Item{
		item{title: "Login", desc: "Fazer login com um Client ID existente", action: 1},
		item{title: "Criar Conta", desc: "Criar nova conta e receber um Client ID", action: 2},
		item{title: "Ping Servidor", desc: "Mede a latência (p)", action: 3},
	}
	m.authList = list.New(authItems, list.NewDefaultDelegate(), 0, 0)
	m.authList.Title = "Menu de Autenticação"

	// --- Main Menu Setup ---
	mainItems := []list.Item{
		item{title: "Ping Servidor", desc: "Mede a latência (p)", action: 1},
		item{title: "Abrir Pack", desc: "Abre um pacote de cartas (o)", action: 2},
		item{title: "Ver Cartas", desc: "Vê sua coleção atual (s)", action: 3},
		item{title: "Procurar Partida", desc: "Entra na fila de matchmaking", action: 4},
		item{title: "Trocar Carta", desc: "Troca uma carta por outra aleatória", action: 5},
	}
	m.mainList = list.New(mainItems, list.NewDefaultDelegate(), 0, 0)
	m.mainList.Title = "Menu Principal"
	m.mainList.SetFilteringEnabled(false)

	// --- Game Menu Setup ---
	m.gameList = list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	m.gameList.Title = "Selecione uma Carta para Jogar"

	return m
}
// --- Commands (Actions/Side-Effects) ---

// CmdPing sends a ping request to the server.
func CmdPing(nc *nats.Conn) tea.Cmd {
	return func() tea.Msg {
		// Use a nova função que retorna a latência e o erro
		latency := pubsub.RequestPing(nc)

		return StatusMsg{isError: false, message: fmt.Sprintf("Ping bem-sucedido! Latência: %dms", latency)}
	}
}

// CmdCreateAccount requests a new user ID.
func CmdCreateAccount(nc *nats.Conn) tea.Cmd {
	return func() tea.Msg {
		clientID := pubsub.RequestCreateAccount(nc)
		if clientID == 0 {
			return StatusMsg{isError: true, message: "Failed to create account. Server may be down."}
		}
		return LoginMsg{clientID: clientID}
	}
}

// CmdLogin simulates a login with a hardcoded ID.
func CmdLogin(nc *nats.Conn, id int) tea.Cmd {
	return func() tea.Msg {
		_, err := pubsub.RequestLogin(nc, id)
		if err != nil {
			return StatusMsg{isError: true, message: fmt.Sprintf("Erro no Login para ID %d: %v", id, err)}
		}
		return LoginMsg{clientID: id}
	}
}

// CmdOpenPack requests a new pack of cards.
func CmdOpenPack(nc *nats.Conn, clientID int) tea.Cmd {
	return func() tea.Msg {
		cards, err := pubsub.RequestOpenPack(nc, clientID)
		if err != nil {
			return StatusMsg{isError: true, message: fmt.Sprintf("Open Pack Error: %v", err)}
		}
		return CardListMsg{cards: cards}
	}
}

// CmdSeeCards requests the user's current card collection.
func CmdSeeCards(nc *nats.Conn, clientID int) tea.Cmd {
	return func() tea.Msg {
		cards, err := pubsub.RequestSeeCards(nc, clientID)
		if err != nil {
			return StatusMsg{isError: true, message: fmt.Sprintf("See Cards Error: %v", err)}
		}
		return CardListMsg{cards: cards}
	}
}

// CmdFindMatch initiates the match finding process.
func CmdFindMatch(nc *nats.Conn, clientID int) tea.Cmd {
	return func() tea.Msg {
		enemyID, err := pubsub.RequestFindMatch(nc, clientID)
		if err != nil {
			return StatusMsg{isError: true, message: fmt.Sprintf("Find Match Error: %v", err)}
		}

		// Requirement: If no error, execute SeeCards first.
		cards, err := pubsub.RequestSeeCards(nc, clientID)
		if err != nil {
			// This is a secondary error, but still important
			return StatusMsg{isError: true, message: fmt.Sprintf("Match found, but couldn't get cards: %v", err)}
		}

		return EnemyFoundMsg{enemyID: enemyID, cards: cards}
	}
}



// CmdTradeCard executa RequestTradeCards
func CmdTradeCard(nc *nats.Conn, clientID int, card int) tea.Cmd {
	return func() tea.Msg {
		newCard, err := pubsub.RequestTradeCards(nc, clientID, card)
		if err != nil {
			return StatusMsg{isError: true, message: fmt.Sprintf("Erro na Troca de Carta %d: %v", card, err)}
		}
		return TradeResultMsg{newCard: newCard}
	}
}

// CmdManageGame runs the game loop in a goroutine and sends the result back.
func CmdManageGame(nc *nats.Conn, clientID int, cardCh chan int, cancel context.CancelFunc) tea.Cmd {
	return func() tea.Msg {
		// ManageGame is blocking until the game ends (win/lose/error)
		result := pubsub.ManageGame(nc, clientID, cardCh)
		var receivedCard int
		if result != "error" {
			receivedCard = <-cardCh
		} else {
			receivedCard = 0 // On error
		}
		return GameUpdateMsg{result: result, card: receivedCard}
	}
}

// CmdWaitForCard is a blocking command that waits for a card to be selected
// in the game menu before sending it via SendCards.
func CmdWaitForCard(nc *nats.Conn, clientID int, cardCh chan int) tea.Cmd {
	return func() tea.Msg {
		// Wait for a card selection from the TUI's Update method
		selectedCard := <-cardCh
		// Now send the card
		pubsub.SendCards(nc, clientID, selectedCard)

		// We return a simple status message to show the card was sent
		return StatusMsg{isError: false, message: fmt.Sprintf("Card %d sent to server. Waiting for game result...", selectedCard)}
	}
}



func (m model) Init() tea.Cmd {
	return tea.Batch(m.checkHeartbeat(), textinput.Blink) // Adiciona o comando para o cursor piscar
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	// --- Global Handlers ---
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			return m, nil
		}
	case tea.WindowSizeMsg:
		h, v := m.authList.Styles.Title.GetVerticalPadding(), m.authList.Styles.Title.GetHorizontalPadding()
		m.authList.SetSize(msg.Width-h, msg.Height-v)
		m.mainList.SetSize(msg.Width-h, msg.Height-v)
		m.gameList.SetSize(msg.Width-h, msg.Height-v)
		m.help.Width = msg.Width
		// Garante que o input também se ajuste
		m.textInput.Width = msg.Width / 3
		m.textInput.Cursor.Blink = false // Previne o erro de piscar

	// --- Custom Message Handlers ---
	case StatusMsg:
		m.statusMsg = msg
		// Não limpa o status automaticamente para facilitar a leitura.
		// return m, tea.Sequence(tea.Tick(timeout, func(t time.Time) tea.Msg {
		// 	return StatusMsg{message: ""}
		// }), nil)
		return m, nil

	case LoginMsg:
		m.clientID = msg.clientID
		m.currentState = mainMenu
		m.statusMsg = StatusMsg{message: fmt.Sprintf("Login bem-sucedido! Client ID: %d. Bem-vindo!", m.clientID)}
		return m, nil

	case CardListMsg:
		m.cards = msg.cards
		cardStr := "Suas Cartas: "
		for _, c := range m.cards {
			cardStr += fmt.Sprintf("%d ", c)
		}
		m.statusMsg = StatusMsg{message: cardStr}
		return m, nil

	case TradeResultMsg:
		m.statusMsg = StatusMsg{message: fmt.Sprintf("Troca bem-sucedida! Recebeu nova carta: %d.", msg.newCard)}
		return m, CmdSeeCards(m.natsConn, m.clientID)

	case EnemyFoundMsg:
		// ... (Lógica de EnemyFoundMsg é a mesma)
		m.enemyID = msg.enemyID
		m.cards = msg.cards
		m.currentState = gameMenu
		m.statusMsg = StatusMsg{message: fmt.Sprintf("Partida Encontrada! Inimigo ID: %d. Selecione uma carta para jogar.", m.enemyID)}

		gameItems := make([]list.Item, len(m.cards))
		for i, card := range m.cards {
			gameItems[i] = item{title: strconv.Itoa(card), desc: "Selecionar esta carta para jogar"}
		}
		m.gameList.SetItems(gameItems)

		_, cancel := context.WithCancel(context.Background())
		m.gameCancel = cancel
		m.gameCardInput = make(chan int, 1)

		cmds = append(cmds, CmdManageGame(m.natsConn, m.clientID, m.gameCardInput, cancel))
		cmds = append(cmds, CmdWaitForCard(m.natsConn, m.clientID, m.gameCardInput))

		return m, tea.Batch(cmds...)

	case GameUpdateMsg:
		// ... (Lógica de GameUpdateMsg é a mesma)
		m.currentState = mainMenu
		if m.gameCancel != nil {
			m.gameCancel()
			m.gameCancel = nil
		}

		switch msg.result {
			case "win":
				m.statusMsg = StatusMsg{message: fmt.Sprintf("🏆 VITÓRIA! Você ganhou a carta %d.", msg.card)}
			case "lose":
				m.statusMsg = StatusMsg{message: fmt.Sprintf("💀 DERROTA! Você perdeu o jogo. Sua carta jogada foi: %d", msg.card)}
			default: // "error"
				m.statusMsg = StatusMsg{isError: true, message: "Erro de Jogo: Saiu da fila/jogo devido a um problema."}
		}

		return m, CmdSeeCards(m.natsConn, m.clientID)

	case tea.Msg:
        return m, m.handleHeartbeatMsg(msg)
	}

	// --- Menu Specific Handlers ---

	switch m.currentState {
	case authMenu:
		return m.handleAuthMenu(msg)
	case mainMenu:
		return m.handleMainMenu(msg)
	case loginForm:
		return m.handleLoginForm(msg) // Novo handler para formulário de login
	case tradeForm:
		return m.handleTradeForm(msg) // Novo handler para formulário de troca
	case gameMenu:
		return m.handleGameMenu(msg)
	}

	return m, cmd
}

func (m model) handleAuthMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd
	m.authList, cmd = m.authList.Update(msg)
    cmds = append(cmds, cmd)

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch {
        case key.Matches(keyMsg, keys.Ping): // <--- NOVO: Ativa o Ping com o atalho 'p'
            cmds = append(cmds, CmdPing(m.natsConn))
        case keyMsg.Type == tea.KeyEnter:
			if selectedItem, ok := m.authList.SelectedItem().(item); ok {
				switch selectedItem.action {
				case 1: // Login (Vai para o formulário)
					m.currentState = loginForm
					m.textInput.Reset()
					m.textInput.Placeholder = "Digite o Client ID..."
					m.textInput.Prompt = "ID: "
					m.textInput.Focus()
					m.statusMsg = StatusMsg{message: "Digite seu Client ID e pressione ENTER."}
					return m, textinput.Blink
				case 2: // Create Account
					return m, CmdCreateAccount(m.natsConn)
				case 3: // Ping (Ação selecionada)
                    cmds = append(cmds, CmdPing(m.natsConn)) // <--- NOVO: Ativa o Ping com Enter
                }
			}
		}
	}
	return m, tea.Batch(cmds...)
}

func (m model) handleMainMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd
	m.mainList, cmd = m.mainList.Update(msg)
	cmds = append(cmds, cmd)

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(keyMsg, keys.Ping):
			cmds = append(cmds, CmdPing(m.natsConn))
		case key.Matches(keyMsg, keys.OpenPack):
			cmds = append(cmds, CmdOpenPack(m.natsConn, m.clientID))
		case key.Matches(keyMsg, keys.SeeCards):
			cmds = append(cmds, CmdSeeCards(m.natsConn, m.clientID))
		case keyMsg.Type == tea.KeyEnter: // Corrigido para tea.KeyEnter
			if selectedItem, ok := m.mainList.SelectedItem().(item); ok {
				switch selectedItem.action {
				case 1: // Ping Server
					cmds = append(cmds, CmdPing(m.natsConn))
				case 2: // Open Pack
					cmds = append(cmds, CmdOpenPack(m.natsConn, m.clientID))
				case 3: // See Cards
					cmds = append(cmds, CmdSeeCards(m.natsConn, m.clientID))
				case 4: // Find Match
					cmds = append(cmds, CmdFindMatch(m.natsConn, m.clientID))
				case 5: // Trade Card (Vai para o formulário)
					if len(m.cards) == 0 {
						m.statusMsg = StatusMsg{isError: true, message: "Não pode trocar: Você não tem cartas!"}
						return m, nil
					}
					m.currentState = tradeForm
					m.textInput.Reset()
					m.textInput.Placeholder = "ID da carta para trocar..."
					m.textInput.Prompt = "Carta ID: "
					m.textInput.Focus()
					m.statusMsg = StatusMsg{message: "Digite o ID de uma de suas cartas para trocar."}
					return m, textinput.Blink
				}
			}
		}
	}
	return m, tea.Batch(cmds...)
}

func (m model) handleGameMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.gameList, cmd = m.gameList.Update(msg)

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyEnter: // Corrigido para tea.KeyEnter
			if selectedItem, ok := m.gameList.SelectedItem().(item); ok {
				cardID, _ := strconv.Atoi(selectedItem.title)
				// Envia a carta selecionada para a goroutine que espera
				m.gameCardInput <- cardID
				return m, nil
			}
		}
	}
	return m, cmd
}

// --- Handlers de Formulário (Novos) ---

func (m model) handleLoginForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			idStr := m.textInput.Value()
			if idStr == "" {
				m.statusMsg = StatusMsg{isError: true, message: "O Client ID não pode estar vazio."}
				return m, nil
			}
			id, err := strconv.Atoi(idStr)
			if err != nil || id <= 0 {
				m.statusMsg = StatusMsg{isError: true, message: "Client ID inválido. Deve ser um número inteiro positivo."}
				return m, nil
			}
			m.textInput.Blur() // Tira o foco do input
			return m, CmdLogin(m.natsConn, id)

		case tea.KeyEsc:
			m.currentState = authMenu
			m.textInput.Blur()
			m.statusMsg = StatusMsg{message: "Login cancelado. Selecione uma opção."}
			return m, nil
		}
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) handleTradeForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			cardStr := m.textInput.Value()
			if cardStr == "" {
				m.statusMsg = StatusMsg{isError: true, message: "O ID da carta não pode estar vazio."}
				return m, nil
			}
			card, err := strconv.Atoi(cardStr)
			if err != nil || card <= 0 {
				m.statusMsg = StatusMsg{isError: true, message: "ID da carta inválido. Deve ser um número inteiro positivo."}
				return m, nil
			}
			
			// Verifica se o usuário possui a carta (simples, apenas verifica se está na lista)
			hasCard := false
			for _, c := range m.cards {
				if c == card {
					hasCard = true
					break
				}
			}
			if !hasCard {
				m.statusMsg = StatusMsg{isError: true, message: fmt.Sprintf("Você não possui a carta com ID %d.", card)}
				return m, nil
			}

			m.textInput.Blur() // Tira o foco do input
			m.currentState = mainMenu // Volta ao menu principal enquanto espera a troca
			return m, CmdTradeCard(m.natsConn, m.clientID, card)

		case tea.KeyEsc:
			m.currentState = mainMenu
			m.textInput.Blur()
			m.statusMsg = StatusMsg{message: "Troca cancelada."}
			return m, nil
		}
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// --- View Refatorada para Incluir Formulários ---

func (m model) View() string {
	s := ""

	// --- Header e Heartbeat ---
	statusColor := "\x1b[32m" // Green
	if time.Now().UnixMilli()-*m.heartbeat > 1000 {
		statusColor = "\x1b[31m" // Red
	}
	s += fmt.Sprintf("NATS Status: %s●\x1b[0m | ", statusColor)
	if m.clientID > 0 {
		s += fmt.Sprintf("Client ID: %d | ", m.clientID)
	}
	s += fmt.Sprintf("Heartbeat: %dms atrás\n\n", time.Now().UnixMilli()-*m.heartbeat)

	// --- Main Content ---
	switch m.currentState {
	case authMenu:
		s += m.authList.View()
	case mainMenu:
		s += m.mainList.View()
	case loginForm:
		s += "🔑 Digite seu Client ID para Login:\n"
		s += m.textInput.View()
		s += "\n\n(ESC para voltar)"
	case tradeForm:
		s += "🔄 Digite o ID da carta que deseja trocar:\n"
		s += m.textInput.View()
		s += fmt.Sprintf("\n\nSuas Cartas: %v", m.cards)
		s += "\n\n(ESC para cancelar)"
	case gameMenu:
		s += fmt.Sprintf("--- ⚔️ EM JOGO ⚔️ ---\nJogando contra Inimigo ID: %d\n\n", m.enemyID)
		s += m.gameList.View()
		s += "\n\n(ENTER para selecionar carta, ESC não funciona em jogo)"
	}

	// --- Status/Footer ---
	s += "\n---\n"
	if m.statusMsg.isError {
		s += fmt.Sprintf("\x1b[31mERRO: %s\x1b[0m\n", m.statusMsg.message)
	} else if m.statusMsg.message != "" {
		s += fmt.Sprintf("\x1b[33mSTATUS: %s\x1b[0m\n", m.statusMsg.message)
	} else {
		s += "Status: OK\n"
	}
	s += m.help.View(keys)

	return s
}

// ... (Restante do código: CmdCreateAccount, CmdPing, CmdOpenPack, CmdSeeCards,
// CmdFindMatch, CmdManageGame, CmdWaitForCard, checkHeartbeat, handleHeartbeatMsg são os mesmos)

// --- Heartbeat Logic ---

// CheckHeartbeatMsg is an internal message to check the NATS heartbeat
type CheckHeartbeatMsg time.Time // Usamos o tempo para garantir que é uma mensagem única

func (m model) checkHeartbeat() tea.Cmd {
    return tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg {
        return CheckHeartbeatMsg(t)
    })
}

func (m *model) handleHeartbeatMsg(msg tea.Msg) tea.Cmd {
    if _, ok := msg.(CheckHeartbeatMsg); ok {
        // Checa se o último ping do servidor foi há mais de 1000ms (1 segundo)
        if time.Now().UnixMilli()-*m.heartbeat > 1000 {
            // Se o heartbeat falhou, o programa deve fechar
            log.Println("Heartbeat do servidor perdido (latência > 1000ms). Fechando o cliente.")
            return tea.Quit
        }
        
        // Se ainda está OK, agenda a próxima checagem
        return m.checkHeartbeat()
    }
    return nil
}

// --- Main Function ---

func _main() {
	// 1. Initial NATS connection and Heartbeat setup
	var serverLastPing int64 = time.Now().UnixMilli()
	nc := pubsub.BrokerConnect(natsServerNumber)
	if nc.Status() != nats.CONNECTED {
		log.Fatalf("Could not connect to NATS server at nats://127.0.0.1:%d", natsServerNumber+4222)
	}
	defer nc.Close()

	// 2. Start the NATS heartbeat listener in a goroutine
	// This goroutine updates serverLastPing
	go pubsub.Heartbeat(nc, &serverLastPing)

	// Wait for the initial connection and first heartbeat update
	// The original loop is replaced by waiting for a short period
	// to ensure the heartbeat goroutine has a chance to run.
	fmt.Println("Attempting to connect and check server status...")
	time.Sleep(time.Second) // Give the heartbeat listener time to subscribe and receive a message

	// 3. Initialize and start the Bubble Tea program
	p := tea.NewProgram(initialModel(nc, &serverLastPing))
	if _, err := p.Run(); err != nil {
		log.Fatalf("Bubble Tea error: %v", err)
	}
}