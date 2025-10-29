package API

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"math/rand" // Importado para backoff randomizado
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hashicorp/raft"
)

// StandardRequest define a estrutura para requisições (NATS e REST Interno)
type StandardRequest struct {
	RequestID     string          `json:"requestId"`
	RequesterID   string          `json:"requesterId"` // ID do follower
	OriginalMsgID string          `json:"originalMsgId,omitempty"`
	Timestamp     int64           `json:"timestamp"`
	OperationType string          `json:"operationType"`
	Payload       json.RawMessage `json:"payload"`
}

// StandardResponse define a estrutura para respostas (NATS e REST Interno)
type StandardResponse struct {
	RequestID    string          `json:"requestId"`
	ResponderID  string          `json:"responderId"` // ID do Líder
	Timestamp    int64           `json:"timestamp"`
	IsSuccess    bool            `json:"isSuccess"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	ErrorCode    string          `json:"errorCode,omitempty"`
	ErrorMessage string          `json:"errorMessage,omitempty"`
}

// NewErrorResponse cria uma resposta de erro padrão
func NewErrorResponse(reqID, responderID, errCode, errMsg string) StandardResponse {
	return StandardResponse{
		RequestID:    reqID,
		ResponderID:  responderID,
		Timestamp:    time.Now().UnixMilli(),
		IsSuccess:    false,
		ErrorCode:    errCode,
		ErrorMessage: errMsg,
	}
}

// NewSuccessResponse cria uma resposta de sucesso padrão
func NewSuccessResponse(reqID, responderID string, payload interface{}) StandardResponse {
	// Tenta serializar o payload. Se falhar, define como nulo ou uma mensagem de erro.
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[NewSuccessResponse WARN] Failed to marshal payload for ReqID '%s': %v\n", reqID, err)
		// Retorna sucesso, mas com payload indicando erro de marshal
		return StandardResponse{
			RequestID:    reqID,
			ResponderID:  responderID,
			Timestamp:    time.Now().UnixMilli(),
			IsSuccess:    true, // A operação principal pode ter sido sucesso, mas a resposta falhou
			Payload:      []byte(fmt.Sprintf(`{"error": "failed to marshal success payload: %v"}`, err)),
		}
	}
	return StandardResponse{
		RequestID:    reqID,
		ResponderID:  responderID,
		Timestamp:    time.Now().UnixMilli(),
		IsSuccess:    true,
		Payload:      payloadBytes,
	}
}

// --- Fim Estruturas Padrão ---

// applyLogInternal (Inalterada - continua necessária para o Líder processar)
// Assume que 'command' está definido em store.go
func (s *Store) applyLogInternal(op string, key string, value string, memberID string, memberAddr string, player *Player, idCount int, cards *[][3]int, gameQueue []int, gameId matchStruct) (any, error) {
	fmt.Println("APPLY INTERNAL")
	if s.RaftLog.State() != raft.Leader {
		return nil, fmt.Errorf("node is not the leader")
	}
	cmd := command{
		Op:         op,
		Key:        key,
		Value:      value,
		MemberID:   memberID,
		MemberAddr: memberAddr,
	}
	if player != nil {
		cmd.Count = idCount
		cmd.PlayerID = player.Id
		cmd.PlayerCards = player.Cards
	}
	if cards!=nil {
		cmd.Cards = *cards
	}
	cmd.GameQueue = gameQueue
	
	cmd.GameId = gameId	
	fmt.Println(cmd.GameId)


	b, err := json.Marshal(cmd)
	if err != nil {
		fmt.Printf("[applyLogInternal ERROR] Marshalling command (%s): %v\n", op, err)
		return nil, fmt.Errorf("failed to marshal command: %w", err)
	}
	fmt.Println("MID1 APPLY INTERNAL")

	applyFuture := s.RaftLog.Apply(b, 500*time.Millisecond)

	fmt.Println("MID2 APPLY INTERNAL")
	
	if err := applyFuture.Error(); err != nil {
		fmt.Printf("[applyLogInternal ERROR] Applying command (%s) to Raft: %v\n", op, err)
		return nil, fmt.Errorf("failed to apply command to Raft: %w", err)
	}

	fmt.Println("APPLY INTERNAL DONE")

	return applyFuture.Response(), nil
}

// --- Função de Forwarding Atualizada ---

// forwardToLeaderViaREST encaminha uma StandardRequest para a API *interna* do líder.
// Retorna uma StandardResponse (mesmo em caso de erro de comunicação).
func (s *Store) forwardToLeaderViaREST(req StandardRequest) StandardResponse {
	retries := 3
	backoff := 100 * time.Millisecond

	// Define o ID do requisitante (este nó follower)
	req.RequesterID = s.NodeID

	for i := 0; i < retries; i++ {
		leaderRaftAddr := string(s.RaftLog.Leader())
		if leaderRaftAddr == "" {
			log.Printf("[Forwarding] Attempt %d/%d for ReqID '%s': No leader known. Waiting...\n", i+1, retries, req.RequestID)
			time.Sleep(backoff + time.Duration(rand.Intn(100))*time.Millisecond)
			backoff *= 2
			continue
		}

		// Assume porta 8080 para API do líder (TODO: Tornar dinâmico)
		leaderHost := strings.Split(leaderRaftAddr, ":")[0]
		leaderAPIPort := 8080
		// O path interno pode ser derivado do OperationType
		// Garante que OperationType seja seguro para URL (ex: 'openPack' -> '/internal/openpack')
		internalPath := fmt.Sprintf("/internal/%s", strings.ToLower(req.OperationType))
		leaderAPIURL := fmt.Sprintf("http://%s:%d%s", leaderHost, leaderAPIPort, internalPath)

		// Serializa a StandardRequest completa para enviar ao líder
		bodyBytes, err := json.Marshal(req)
		if err != nil {
			log.Printf("[Forwarding] ERROR for ReqID '%s': Failed marshalling request: %v\n", req.RequestID, err)
			return NewErrorResponse(req.RequestID, s.NodeID, "FORWARD_MARSHAL_ERROR", err.Error())
		}

		log.Printf("[Forwarding] Attempt %d/%d for ReqID '%s': Forwarding %s to leader API at %s\n", i+1, retries, req.RequestID, req.OperationType, leaderAPIURL)

		// Cria e envia a requisição HTTP POST (assume POST para operações internas que mudam estado)
		// Para Ping (GET), esta função precisaria ser adaptada ou uma nova criada
		proxyReq, err := http.NewRequest("POST", leaderAPIURL, bytes.NewBuffer(bodyBytes))
		if err != nil {
			log.Printf("[Forwarding] ERROR for ReqID '%s': Failed creating request: %v\n", req.RequestID, err)
			return NewErrorResponse(req.RequestID, s.NodeID, "FORWARD_REQUEST_CREATE_ERROR", err.Error())
		}
		proxyReq.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(proxyReq)

		// Analisa erro de comunicação
		if err != nil {
			log.Printf("[Forwarding] WARN Attempt %d/%d for ReqID '%s': Error sending request to leader %s: %v. Retrying after backoff...\n", i+1, retries, req.RequestID, leaderAPIURL, err)
			time.Sleep(backoff + time.Duration(rand.Intn(100))*time.Millisecond)
			backoff *= 2
			continue // Tenta novamente
		}

		// Lê a resposta do líder (espera uma StandardResponse)
		responseBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if readErr != nil {
			log.Printf("[Forwarding] ERROR for ReqID '%s': Failed reading response body from leader %s: %v\n", req.RequestID, leaderAPIURL, readErr)
			return NewErrorResponse(req.RequestID, s.NodeID, "FORWARD_READ_RESPONSE_ERROR", readErr.Error())
		}

		// Deserializa a StandardResponse do líder
		var leaderResp StandardResponse
		if err := json.Unmarshal(responseBody, &leaderResp); err != nil {
			// Se o líder não retornou o formato esperado
			log.Printf("[Forwarding] ERROR for ReqID '%s': Failed unmarshalling leader response (Status: %d): %v. Body: %s\n", req.RequestID, resp.StatusCode, err, string(responseBody))
			return NewErrorResponse(req.RequestID, s.NodeID, "FORWARD_UNMARSHAL_ERROR", fmt.Sprintf("Leader returned status %d but invalid response format", resp.StatusCode))
		}

		log.Printf("[Forwarding] Success for ReqID '%s': Received response from leader %s (Status: %d). IsSuccess: %t\n", req.RequestID, leaderAPIURL, resp.StatusCode, leaderResp.IsSuccess)
		// Retorna a resposta recebida do líder
		fmt.Println(string(leaderResp.Payload))
		fmt.Println(s.Cards)
		return leaderResp

	} // Fim do loop for retries

	// Se chegou aqui, todas as tentativas falharam
	log.Printf("[Forwarding] FATAL ERROR for ReqID '%s': Failed to forward request to leader after %d attempts.\n", req.RequestID, retries)
	return NewErrorResponse(req.RequestID, s.NodeID, "FORWARD_FAILED_MAX_RETRIES", fmt.Sprintf("failed to forward request after %d attempts", retries))
}

// --- Handlers REST (Externos e Internos Atualizados) ---

// statusHandler (Inalterado)
func (s *Store) statusHandler(c *gin.Context) {
	state := s.RaftLog.State().String()
	leader := s.RaftLog.Leader()
	members := s.GetMembers() // Assume GetMembers em store.go

	c.JSON(http.StatusOK, gin.H{
		"node_id": s.NodeID,
		"state":   state,
		"leader":  leader,
		"address": s.RaftAddr,
		"members": members,
	})
}

// joinHandler (Inalterado - Usa Redirect)
func (s *Store) joinHandler(c *gin.Context) {
	var req struct {
		ID      string `json:"id" binding:"required"`
		Address string `json:"address" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid join request: 'id' and 'address' are required"})
		return
	}

	if s.RaftLog.State() != raft.Leader {
		leaderAddr := string(s.RaftLog.Leader())
		log.Printf("[API Join] Not leader, redirecting join request from '%s' to leader: %s\n", req.ID, leaderAddr)
		c.JSON(http.StatusTemporaryRedirect, gin.H{
			"error":  "Not the leader. Redirect to the leader to join.",
			"leader": leaderAddr,
		})
		return
	}

	log.Printf("[API Join] Received join request from Node '%s' at '%s'\n", req.ID, req.Address)

	addVoterFuture := s.RaftLog.AddVoter(raft.ServerID(req.ID), raft.ServerAddress(req.Address), 0, 0)
	if err := addVoterFuture.Error(); err != nil {
		log.Printf("[API Join] Error adding voter '%s' to Raft config: %v\n", req.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to add voter to Raft configuration: %v", err)})
		return
	}
	log.Printf("[API Join] Raft AddVoter successful for Node '%s'\n", req.ID)

	_, err := s.applyLogInternal("add_member", "", "", req.ID, req.Address, nil, 0, nil, []int{}, matchStruct{})
	if err != nil {
		log.Printf("[API Join] Error applying 'add_member' to FSM for Node '%s': %v (Raft AddVoter succeeded)\n", req.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to apply member addition to FSM: %v", err)})
		return
	}

	log.Printf("[API Join] FSM 'add_member' successfully applied for Node '%s'\n", req.ID)
	c.JSON(http.StatusOK, gin.H{"status": "Node added successfully to Raft and FSM"})
}

// leaveHandler (Inalterado - Usa Redirect)
func (s *Store) leaveHandler(c *gin.Context) {
	if s.RaftLog.State() != raft.Leader {
		leaderAddr := string(s.RaftLog.Leader())
		log.Printf("[API Leave] Not leader, redirecting leave request to leader: %s\n", leaderAddr)
		c.JSON(http.StatusTemporaryRedirect, gin.H{
			"error":  "Not the leader. Redirect to the leader to leave.",
			"leader": leaderAddr,
		})
		return
	}

	var req struct {
		ID string `json:"id" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid leave request: 'id' is required"})
		return
	}

	log.Printf("[API Leave] Received leave request for Node '%s'\n", req.ID)

	removeFuture := s.RaftLog.RemoveServer(raft.ServerID(req.ID), 0, 0)
	if err := removeFuture.Error(); err != nil {
		log.Printf("[API Leave] Error removing server '%s' from Raft config: %v\n", req.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to remove server from Raft: %v", err)})
		return
	}
	log.Printf("[API Leave] Raft RemoveServer successful for Node '%s'\n", req.ID)

	_, err := s.applyLogInternal("remove_member", "", "", req.ID, "", nil, 0, nil,[]int{}, matchStruct{})
	if err != nil {
		log.Printf("[API Leave] Error applying 'remove_member' to FSM for Node '%s': %v (Raft RemoveServer succeeded)\n", req.ID, err)
		// Continua
	} else {
		log.Printf("[API Leave] FSM 'remove_member' successfully applied for Node '%s'\n", req.ID)
	}

	c.JSON(http.StatusOK, gin.H{"status": "Node removed successfully from Raft (FSM update applied/attempted)"})
}

// membersHandler (Inalterado)
func (s *Store) membersHandler(c *gin.Context) {
	members := s.GetMembers() // Assume GetMembers em store.go
	c.JSON(http.StatusOK, gin.H{
		"queried_node_id": s.NodeID,
		"members":         members,
	})
}

// getHandler (Inalterado)
func (s *Store) getHandler(c *gin.Context) {
	key := c.Param("key")
	s.mu.Lock() // Usa a mu do Store definido em store.go
	value, ok := s.data[key]
	s.mu.Unlock()
	if ok {
		c.JSON(http.StatusOK, gin.H{"key": key, "value": value})
	} else {
		c.JSON(http.StatusNotFound, gin.H{"error": "key not found"})
	}
}

// setHandler (Inalterado - Usa Redirect)
func (s *Store) setHandler(c *gin.Context) {
	var req struct {
		Value string `json:"value" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload: 'value' is required"})
		return
	}
	key := c.Param("key")

	_, err := s.applyLogInternal("set", key, req.Value, "", "", nil, 0, nil,[]int{}, matchStruct{})
	if err != nil {
		if err.Error() == "node is not the leader" {
			leaderAddr := string(s.RaftLog.Leader())
			log.Printf("[API Set] Not leader, redirecting client to leader: %s\n", leaderAddr)
			c.JSON(http.StatusTemporaryRedirect, gin.H{
				"error":  "Not the leader. Redirect to the leader for write operations.",
				"leader": leaderAddr,
			})
		} else {
			log.Printf("[API Set] Error applying log for key '%s': %v\n", key, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to apply log: %v", err)})
		}
		return
	}
	log.Printf("[API Set] Successfully applied 'set' for key '%s'\n", key)
	c.JSON(http.StatusOK, gin.H{"status": "applied", "key": key, "value": req.Value})
}

// --- Handler Interno (Atualizado para StandardResponse) ---

// handleInternalPing responde a pings internos do follower
func (s *Store) handleInternalPing(c *gin.Context) {
	// Obtém o RequestID da query param (ou gera um se não houver)
	// *** ALTERAÇÃO: Usa um ID padrão se não fornecido ***
	reqID := c.DefaultQuery("requestId", fmt.Sprintf("internal-ping-%d", time.Now().UnixNano()))

	if s.RaftLog.State() != raft.Leader {
		log.Printf("[Internal Ping WARN] Received internal ping (ReqID: %s), but not leader.\n", reqID)
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse(reqID, s.NodeID, "NOT_LEADER", "This node is not the leader"))
		return
	}

	log.Printf("[Internal Ping] Received internal ping (ReqID: %s). Responding pong.\n", reqID)
	// Usa NewSuccessResponse para formatar a resposta
	respPayload := gin.H{"status": "pong", "leaderId": s.NodeID}
	// *** ALTERAÇÃO: Usa NewSuccessResponse consistentemente ***
	c.JSON(http.StatusOK, NewSuccessResponse(reqID, s.NodeID, respPayload))
}

func (s *Store) handleInternalCreatePlayer(c *gin.Context) {
	var req StandardRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("none", s.NodeID, "INVALID_JSON", err.Error()))
		return
	}

	var player Player
	if err := json.Unmarshal(req.Payload, &player); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse(req.RequestID, s.NodeID, "INVALID_PLAYER", err.Error()))
		return
	}

	if s.RaftLog.State() != raft.Leader {
		resp := s.forwardToLeaderViaREST(req)
		c.JSON(http.StatusOK, resp)
		return
	}

	playerID, err := s.CreatePlayer()

	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse(req.RequestID, s.NodeID, "RAFT_APPLY_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, NewSuccessResponse(req.RequestID, s.NodeID, gin.H{"status": "player created","player_id": playerID,}))
}

func (s *Store) handleLogin(c *gin.Context) {
	var req StandardRequest

	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("none", s.NodeID, "INVALID_JSON", err.Error()))
		return
	}
	var data struct {
		ClientID int `json:"client_id"`
	}
	json.Unmarshal(req.Payload, &data)
	fmt.Println(data)
	logged := s.checkIfAnyNodeLogged(data.ClientID)

	c.JSON(http.StatusOK, NewSuccessResponse("none", s.NodeID, gin.H{
		"logged": logged,
	}))
}

func (s *Store) handleInternalIsLogged(c *gin.Context) {
	var req struct {
		ClientID int `json:"client_id"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("none", s.NodeID, "INVALID_JSON", err.Error()))
		return
	}

	logged := isLogged(req.ClientID)
	fmt.Println("algooo")
	c.JSON(http.StatusOK, NewSuccessResponse("none", s.NodeID, gin.H{
		"logged": logged,
	}))
}


func (s *Store) checkIfAnyNodeLogged(clientID int) bool {
	s.mu.Lock()
	members := make(map[string]raft.ServerAddress)
	maps.Copy(members, s.members)
	s.mu.Unlock()

	// fmt.Println("COPIOU") // Removido para não poluir o log

	// respData pode ser definido aqui, pois é usado apenas localmente.
	type respData struct {
		Logged bool `json:"logged"`
	}

	// 1. Contexto com cancelamento.
	// Quando esta função retornar (seja com true ou false),
	// o defer cancel() será chamado, cancelando todas as
	// requisições HTTP que ainda estiverem em andamento.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	// Canal com buffer 1. Apenas a *primeira* goroutine a encontrar
	// o resultado 'true' conseguirá escrever nele.
	resultChan := make(chan bool, 1)

	// Criamos um único cliente HTTP para ser reutilizado
	client := &http.Client{
		Timeout: 600 * time.Millisecond,
	}

	wg.Add(len(members))
	for nodeID, addr := range members {
		// Criamos as variáveis aqui para que sejam capturadas
		// corretamente pela goroutine (Evita o "Loop Variable Trap")
		go func(nodeID string, addr raft.ServerAddress) {
			defer wg.Done()

			host, _, _ := net.SplitHostPort(string(addr))
			url := fmt.Sprintf("http://%s/internal/is_logged", net.JoinHostPort(host, strconv.Itoa(8080)))
			fmt.Println("Consultando:", url)

			body, _ := json.Marshal(map[string]int{"client_id": clientID})

			// 2. Criamos a requisição com o contexto
			req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
			if err != nil {
				// Se o contexto já foi cancelado, nem tentamos
				if !errors.Is(err, context.Canceled) {
					fmt.Printf("[WARN] Falha ao criar requisição para %s: %v\n", nodeID, err)
				}
				return
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				// 3. Se o erro for de contexto cancelado, é esperado.
				// Só logamos se for um erro "real" (ex: connection refused)
				if !errors.Is(err, context.Canceled) {
					fmt.Printf("[WARN] Falha ao consultar %s (%s): %v\n", nodeID, addr, err)
				}
				return
			}
			defer resp.Body.Close()

			var standard StandardResponse
			if err := json.NewDecoder(resp.Body).Decode(&standard); err != nil {
				fmt.Printf("[WARN] Resposta inválida de %s: %v\n", nodeID, err)
				return
			}
			var data respData
			json.Unmarshal(standard.Payload, &data)
			fmt.Println("Resultado de", nodeID, ":", data.Logged)

			if data.Logged {
				// 4. Envia 'true' para o canal.
				// Graças ao select, se o canal já tiver recebido um 'true'
				// (e estiver cheio), cairemos no 'default' e não bloquearemos.
				select {
				case resultChan <- true:
					// Fomos os primeiros a relatar 'true'
				default:
					// Alguém já relatou 'true', apenas terminamos
				}
			}
		}(nodeID, addr)
	}

	// 5. Lança uma goroutine para esperar todas as outras terminarem
	// e então fechar o canal. Isso sinaliza que "todos terminaram".
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// 6. Espera por um resultado.
	// Se resultChan for fechado (wg.Wait() terminou), 'ok' será 'false'.
	// Se recebermos 'true', 'ok' será 'true' e 'result' será 'true'.
	result, ok := <-resultChan

	// Retorna true apenas se ok=true E result=true
	return ok && result
}

func (s *Store) handleInternalOpenPack(c *gin.Context) {
	var req StandardRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("none", s.NodeID, "INVALID_JSON", err.Error()))
		return
	}

	var player struct {
		ClientID int `json:"client_id"`
	}
	if err := json.Unmarshal(req.Payload, &player); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse(req.RequestID, s.NodeID, "INVALID_PLAYER", err.Error()))
		return
	}
	fmt.Println(player)
	if s.RaftLog.State() != raft.Leader {
		resp := s.forwardToLeaderViaREST(req)
		c.JSON(http.StatusOK, resp)
		return
	}


	result, err := s.OpenPack(player.ClientID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse(req.RequestID, s.NodeID, "RAFT_APPLY_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, NewSuccessResponse(req.RequestID, s.NodeID, gin.H{"status": "pack open","player_id": player.ClientID,"result":result}))
}

func (s *Store) handleInternalJoinGameQueue(c *gin.Context) {
	var req StandardRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("none", s.NodeID, "INVALID_JSON", err.Error()))
		return
	}

	var player struct {
		ClientID int `json:"client_id"`
	}
	if err := json.Unmarshal(req.Payload, &player); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse(req.RequestID, s.NodeID, "INVALID_PLAYER", err.Error()))
		return
	}
	fmt.Println(player)
	if s.RaftLog.State() != raft.Leader {
		resp := s.forwardToLeaderViaREST(req)
		c.JSON(http.StatusOK, resp)
		return
	}


	result, err := s.JoinQueue(player.ClientID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse(req.RequestID, s.NodeID, "RAFT_APPLY_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, NewSuccessResponse(req.RequestID, s.NodeID, gin.H{"status": "Added to queue","player_id": result,"err":nil}))
}


func (s *Store) handleMatchmaking(c *gin.Context) {
	var req StandardRequest

	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("none", s.NodeID, "INVALID_JSON", err.Error()))
		return
	}
	x, _ := s.CreateMatch()
	ready1 := s.checkMatchmaking(x.P1, x)
	ready2 := s.checkMatchmaking(x.P2, x)

	c.JSON(http.StatusOK, NewSuccessResponse("none", s.NodeID, gin.H{
		"ready1": ready1,
		"ready2": ready2,
		"p1" : x.P1,
		"p2" : x.P2,
	}))
}

func (s *Store) handleInternalReadyMatchmaking(c *gin.Context) {
	var req struct {
		ClientID int `json:"client_id"`
		Match matchStruct `json:"match"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("none", s.NodeID, "INVALID_JSON", err.Error()))
		return
	}

	ready := readyToPlay(req.ClientID, req.Match) //GET THIS
	fmt.Println("algooo")
	c.JSON(http.StatusOK, NewSuccessResponse("none", s.NodeID, gin.H{
		"ready": ready,
	}))
}


func (s *Store) checkMatchmaking(clientID int, game matchStruct) bool {
	s.mu.Lock()
	members := make(map[string]raft.ServerAddress)
	maps.Copy(members, s.members)
	s.mu.Unlock()

	type respData struct {
		Ready bool `json:"ready"`
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	// Canal com buffer 1. Apenas a *primeira* goroutine a encontrar
	// o resultado 'true' conseguirá escrever nele.
	resultChan := make(chan bool, 1)

	// Criamos um único cliente HTTP para ser reutilizado
	client := &http.Client{
		Timeout: 600 * time.Millisecond,
	}

	wg.Add(len(members))
	for nodeID, addr := range members {
		// Criamos as variáveis aqui para que sejam capturadas
		// corretamente pela goroutine (Evita o "Loop Variable Trap")
		go func(nodeID string, addr raft.ServerAddress) {
			defer wg.Done()

			host, _, _ := net.SplitHostPort(string(addr))
			url := fmt.Sprintf("http://%s/internal/ready_matchmaking", net.JoinHostPort(host, strconv.Itoa(8080)))
			fmt.Println("Consultando:", url)

			body, _ := json.Marshal(map[string]any{"client_id": clientID,"match": game})

			// 2. Criamos a requisição com o contexto
			req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
			if err != nil {
				// Se o contexto já foi cancelado, nem tentamos
				if !errors.Is(err, context.Canceled) {
					fmt.Printf("[WARN] Falha ao criar requisição para %s: %v\n", nodeID, err)
				}
				return
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				// 3. Se o erro for de contexto cancelado, é esperado.
				// Só logamos se for um erro "real" (ex: connection refused)
				if !errors.Is(err, context.Canceled) {
					fmt.Printf("[WARN] Falha ao consultar %s (%s): %v\n", nodeID, addr, err)
				}
				return
			}
			defer resp.Body.Close()

			var standard StandardResponse
			if err := json.NewDecoder(resp.Body).Decode(&standard); err != nil {
				fmt.Printf("[WARN] Resposta inválida de %s: %v\n", nodeID, err)
				return
			}
			var data respData
			json.Unmarshal(standard.Payload, &data)
			fmt.Println("Resultado de", nodeID, ":", data.Ready)

			if data.Ready {
				// 4. Envia 'true' para o canal.
				// Graças ao select, se o canal já tiver recebido um 'true'
				// (e estiver cheio), cairemos no 'default' e não bloquearemos.
				select {
				case resultChan <- true:
					// Fomos os primeiros a relatar 'true'
				default:
					// Alguém já relatou 'true', apenas terminamos
				}
			}
		}(nodeID, addr)
	}

	// 5. Lança uma goroutine para esperar todas as outras terminarem
	// e então fechar o canal. Isso sinaliza que "todos terminaram".
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// 6. Espera por um resultado.
	// Se resultChan for fechado (wg.Wait() terminou), 'ok' será 'false'.
	// Se recebermos 'true', 'ok' será 'true' e 'result' será 'true'.
	result, ok := <-resultChan

	// Retorna true apenas se ok=true E result=true
	return ok && result
}

func (s *Store) handleInternalPlayCards(c *gin.Context) {
	var req StandardRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("none", s.NodeID, "INVALID_JSON", err.Error()))
		return
	}

	var play struct {
		ClientID int `json:"client_id"`
		Card int `json:"card"`
		GameID string `json:"game"`
	}
	if err := json.Unmarshal(req.Payload, &play); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse(req.RequestID, s.NodeID, "INVALID_PLAYER", err.Error()))
		return
	}
	fmt.Println(play)
	if s.RaftLog.State() != raft.Leader {
		resp := s.forwardToLeaderViaREST(req)
		c.JSON(http.StatusOK, resp)
		return
	}


	err := s.PlayCard(play.GameID, play.ClientID, play.Card)

	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse(req.RequestID, s.NodeID, "RAFT_APPLY_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, NewSuccessResponse(req.RequestID, s.NodeID, gin.H{"status": "card played"}))
}

func (s *Store) handleSendGameResult(c *gin.Context) {
	var req StandardRequest

	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("none", s.NodeID, "INVALID_JSON", err.Error()))
		return
	}

	var play struct {
		ClientID int `json:"client_id"`
		Card int `json:"card"`
		GameID string `json:"game"`
	}
	if err := json.Unmarshal(req.Payload, &play); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse(req.RequestID, s.NodeID, "INVALID_PLAYER", err.Error()))
		return
	}

	temp:=s.matchHistory[play.GameID]
	var response1, response2 map[string]any
	if temp.Card1>temp.Card2 {
		response1 = map[string]any{
			"client_id" : temp.P1,
			"result"	: "win",
			"card"		: temp.Card2,
		}
		response2 = map[string]any{
			"client_id" : temp.P2,
			"result"	: "lose",
			"card"		: temp.Card1,
		}
	}else {
		response1 = map[string]any{
			"client_id" : temp.P1,
			"result"	: "lose",
			"card"		: temp.Card2,
		}
		response2 = map[string]any{
			"client_id" : temp.P2,
			"result"	: "win",
			"card"		: temp.Card1,
		}
	}
	s.checkGame(response1)
	s.checkGame(response2)

	c.JSON(http.StatusOK, NewSuccessResponse("none", s.NodeID, gin.H{
		"done": false,
	}))
}

func (s *Store) handleInternalCheckGame(c *gin.Context) {
	var req map[string]any
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("none", s.NodeID, "INVALID_JSON", err.Error()))
		return
	}

	SendingGameResult(req)
	fmt.Println("algooo")
	c.JSON(http.StatusOK, NewSuccessResponse("none", s.NodeID, gin.H{
		"ready":false,
	}))
}

func (s *Store) checkGame(response map[string]any) bool {
	s.mu.Lock()
	members := make(map[string]raft.ServerAddress)
	maps.Copy(members, s.members)
	s.mu.Unlock()

	type respData struct {
		Ready bool `json:"ready"`
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	// Canal com buffer 1. Apenas a *primeira* goroutine a encontrar
	// o resultado 'true' conseguirá escrever nele.
	resultChan := make(chan bool, 1)

	// Criamos um único cliente HTTP para ser reutilizado
	client := &http.Client{
		Timeout: 600 * time.Millisecond,
	}

	wg.Add(len(members))
	for nodeID, addr := range members {
		// Criamos as variáveis aqui para que sejam capturadas
		// corretamente pela goroutine (Evita o "Loop Variable Trap")
		go func(nodeID string, addr raft.ServerAddress) {
			defer wg.Done()

			host, _, _ := net.SplitHostPort(string(addr))
			url := fmt.Sprintf("http://%s/internal/check_game", net.JoinHostPort(host, strconv.Itoa(8080)))
			fmt.Println("Consultando:", url)

			body, _ := json.Marshal(response)

			// 2. Criamos a requisição com o contexto
			req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
			if err != nil {
				// Se o contexto já foi cancelado, nem tentamos
				if !errors.Is(err, context.Canceled) {
					fmt.Printf("[WARN] Falha ao criar requisição para %s: %v\n", nodeID, err)
				}
				return
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				// 3. Se o erro for de contexto cancelado, é esperado.
				// Só logamos se for um erro "real" (ex: connection refused)
				if !errors.Is(err, context.Canceled) {
					fmt.Printf("[WARN] Falha ao consultar %s (%s): %v\n", nodeID, addr, err)
				}
				return
			}
			defer resp.Body.Close()

			var standard StandardResponse
			if err := json.NewDecoder(resp.Body).Decode(&standard); err != nil {
				fmt.Printf("[WARN] Resposta inválida de %s: %v\n", nodeID, err)
				return
			}
			var data respData
			json.Unmarshal(standard.Payload, &data)
			fmt.Println("Resultado de", nodeID, ":", data.Ready)

			if data.Ready {
				// 4. Envia 'true' para o canal.
				// Graças ao select, se o canal já tiver recebido um 'true'
				// (e estiver cheio), cairemos no 'default' e não bloquearemos.
				select {
				case resultChan <- true:
					// Fomos os primeiros a relatar 'true'
				default:
					// Alguém já relatou 'true', apenas terminamos
				}
			}
		}(nodeID, addr)
	}

	// 5. Lança uma goroutine para esperar todas as outras terminarem
	// e então fechar o canal. Isso sinaliza que "todos terminaram".
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// 6. Espera por um resultado.
	// Se resultChan for fechado (wg.Wait() terminou), 'ok' será 'false'.
	// Se recebermos 'true', 'ok' será 'true' e 'result' será 'true'.
	result, ok := <-resultChan

	// Retorna true apenas se ok=true E result=true
	return ok && result
}











































// --- Handler de Debug (Atualizado para usar StandardResponse e chamada GET) ---

// handleDebugPingLeader dispara o ping para o líder e retorna o RTT.
func (s *Store) handleDebugPingLeader(c *gin.Context) {
	// Gera um ID para esta operação de debug
	debugReqID := fmt.Sprintf("debug-ping-%d", time.Now().UnixNano())
	log.Printf("[Debug Ping] Received request (ReqID: %s) to ping the leader from node %s.\n", debugReqID, s.NodeID)

	if s.RaftLog.State() == raft.Leader {
		log.Println("[Debug Ping] This node is the leader. Responding directly.")
		// Resposta consistente, mesmo sendo o líder
		c.JSON(http.StatusOK, gin.H{
			"requestId":         debugReqID,
			"status":            "pong_from_self",
			"message":           "This node is the leader.",
			"queried_node_id":   s.NodeID,
			"queried_node_addr": s.RaftAddr,
			"ping_rtt_ms":       0.0,
			"leader_response":   NewSuccessResponse(debugReqID, s.NodeID, gin.H{"status": "pong", "leaderId": s.NodeID}), // Inclui uma resposta padrão
		})
		return
	}

	leaderRaftAddr := string(s.RaftLog.Leader())
	if leaderRaftAddr == "" {
		log.Println("[Debug Ping ERROR] No leader known.")
		// *** ALTERAÇÃO: Retorna StandardResponse no erro ***
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse(debugReqID, s.NodeID, "NO_LEADER_KNOWN", "No leader known by this follower"))
		return
	}

	// Assume porta 8080 (TODO: Tornar dinâmico)
	leaderHost := strings.Split(leaderRaftAddr, ":")[0]
	leaderAPIPort := 8080
	// *** ALTERAÇÃO: Passa requestId como query param para o GET ***
	leaderPingURL := fmt.Sprintf("http://%s:%d/internal/ping?requestId=%s", leaderHost, leaderAPIPort, debugReqID)

	log.Printf("[Debug Ping] Sending internal ping request (ReqID: %s) to leader at %s...\n", debugReqID, leaderPingURL)

	client := &http.Client{Timeout: 3 * time.Second}
	startTime := time.Now()
	// *** ALTERAÇÃO: Usa http.Get para o endpoint /internal/ping ***
	resp, err := client.Get(leaderPingURL)
	endTime := time.Now()
	pingRTT := endTime.Sub(startTime).Seconds() * 1000

	// Trata erro de comunicação
	if err != nil {
		log.Printf("[Debug Ping ERROR] Failed sending ping (ReqID: %s) to leader %s: %v\n", debugReqID, leaderPingURL, err)
		// *** ALTERAÇÃO: Retorna estrutura de erro mais consistente ***
		c.JSON(http.StatusBadGateway, gin.H{
			"requestId":    debugReqID,
			"error_code":   "LEADER_UNREACHABLE",
			"error_message":fmt.Sprintf("Failed to reach leader at %s", leaderPingURL),
			"details":      err.Error(),
			"ping_rtt_ms":  pingRTT,
			"current_node": s.NodeID,
		})
		return
	}
	defer resp.Body.Close()

	// Lê resposta do líder
	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		log.Printf("[Debug Ping ERROR] Failed reading response body (ReqID: %s) from leader %s: %v\n", debugReqID, leaderPingURL, readErr)
		// *** ALTERAÇÃO: Retorna estrutura de erro mais consistente ***
		c.JSON(http.StatusInternalServerError, gin.H{
			"requestId":    debugReqID,
			"error_code":   "LEADER_RESPONSE_READ_ERROR",
			"error_message":"Failed to read response body from leader",
			"leader_url":   leaderPingURL,
			"status_code":  resp.StatusCode,
			"ping_rtt_ms":  pingRTT,
			"current_node": s.NodeID,
		})
		return
	}

	log.Printf("[Debug Ping] Received response (ReqID: %s) from leader %s. Status: %d. RTT: %.2f ms\n",
		debugReqID, leaderPingURL, resp.StatusCode, pingRTT)

	// Tenta deserializar a StandardResponse do líder
	var leaderResp StandardResponse
	if err := json.Unmarshal(bodyBytes, &leaderResp); err != nil {
		// Se líder não retornou StandardResponse, mostra erro e raw body
		log.Printf("[Debug Ping WARN] Leader response (ReqID: %s) was not a StandardResponse: %v. Body: %s\n", debugReqID, err, string(bodyBytes))
		c.JSON(http.StatusOK, gin.H{ // Retorna 200 OK, mas indica o problema na resposta
			"requestId":    debugReqID,
			"ping_rtt_ms":  pingRTT,
			"leader_url":   leaderPingURL,
			"leader_code":  resp.StatusCode,
			"leader_resp":  gin.H{"raw_response": string(bodyBytes)}, // Retorna raw
			"current_node": s.NodeID,
			"warning":      "Leader response format was not StandardResponse",
		})
		return
	}

	// Monta a resposta final incluindo o RTT e a StandardResponse do líder
	finalResponse := gin.H{
		"requestId":    debugReqID,
		"ping_rtt_ms":  pingRTT,
		"leader_url":   leaderPingURL,
		"leader_code":  resp.StatusCode, // Código HTTP da resposta do líder
		"leader_resp":  leaderResp,      // A StandardResponse completa do líder
		"current_node": s.NodeID,
	}
	// Usa o status code retornado pelo líder para a resposta final ao cliente
	c.JSON(resp.StatusCode, finalResponse)
}

// SetupRouter configura as rotas HTTP, incluindo as novas.
func SetupRouter(s *Store) *gin.Engine {
	// gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// Endpoints Externos/Públicos
	r.GET("/status", s.statusHandler)
	r.POST("/join", s.joinHandler)
	r.POST("/leave", s.leaveHandler)
	r.GET("/members", s.membersHandler)
	r.GET("/data/:key", s.getHandler)
	r.POST("/data/:key", s.setHandler)
	// Endpoint de Debug
	r.GET("/debug/ping-leader", s.handleDebugPingLeader)

	// Endpoints Internos (para comunicação Servidor-Servidor)
	internalGroup := r.Group("/internal")
	{
		// Endpoint Interno de Ping (agora GET)
		internalGroup.GET("/ping", s.handleInternalPing)
		internalGroup.POST("/create_player", s.handleInternalCreatePlayer)
		internalGroup.POST("/is_logged", s.handleInternalIsLogged)
		internalGroup.POST("/login", s.handleLogin)
		internalGroup.POST("/open_pack", s.handleInternalOpenPack)
		internalGroup.POST("/join_game_queue", s.handleInternalJoinGameQueue)
		internalGroup.POST("/ready_matchmaking", s.handleInternalReadyMatchmaking)
		internalGroup.POST("/matchmaking", s.handleMatchmaking)
		internalGroup.POST("/play_cards", s.handleInternalPlayCards)
		internalGroup.POST("/check_game", s.handleInternalCheckGame)
		internalGroup.POST("/send_game_result", s.handleSendGameResult)
		// Adicionar outros endpoints internos aqui (ex: POST /internal/openPack usando StandardRequest/StandardResponse)
		// internalGroup.POST("/openpack", s.handleInternalOpenPack) // Exemplo
	}

	log.Println("Gin router configured with external, internal, and debug routes.")
	return r
}

