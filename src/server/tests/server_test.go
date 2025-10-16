package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// Testa o GET/SET básico através da API do líder
func TestLeaderWriteAndRead(t *testing.T) {
	// Assume que o cluster Raft está rodando (Node1 é o líder)
	leaderPort := 8080 
	key := "testkey"
	value := "testvalue"

	// 1. Tentar SET (Escrever)
	t.Log("Tentando escrever no líder...")
	url := fmt.Sprintf("http://localhost:%d/data/%s", leaderPort, key)
	payload := []byte(fmt.Sprintf(`{"value": "%s"}`, value))

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		t.Fatalf("Falha ao enviar POST para o líder: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Esperado status 200 para SET, obtido %d. Body: %s", resp.StatusCode, body)
	}

	// 2. Tentar GET (Ler)
	t.Log("Esperando um pouco pela replicação e tentando ler...")
	time.Sleep(500 * time.Millisecond) // Pequeno atraso para replicação

	resp, err = http.Get(url)
	if err != nil {
		t.Fatalf("Falha ao enviar GET para o líder: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Esperado status 200 para GET, obtido %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)

	if result["value"] != value {
		t.Errorf("Valor lido incorreto. Esperado: %s, Obtido: %s", value, result["value"])
	}
	t.Logf("Teste de escrita/leitura do líder bem-sucedido.")
}


// Testa a leitura de um seguidor
func TestFollowerRead(t *testing.T) {
	// Assume que Node3 (porta 8082) é um seguidor e Node1 (porta 8080) é o líder
	followerPort := 8082
	leaderPort := 8080
	key := "follower_test_key"
	value := "follower_read_value"

	// Pré-condição: Escrever no líder (Node1)
	writeURL := fmt.Sprintf("http://localhost:%d/data/%s", leaderPort, key)
	writePayload := []byte(fmt.Sprintf(`{"value": "%s"}`, value))
	_, err := http.Post(writeURL, "application/json", bytes.NewBuffer(writePayload))
	if err != nil {
		t.Fatalf("Falha na pré-condição (escrita no líder): %v", err)
	}

	// 1. Tentar GET (Ler) no seguidor
	t.Log("Esperando replicação e tentando ler no seguidor...")
	time.Sleep(1 * time.Second) // Atraso para garantir a replicação

	readURL := fmt.Sprintf("http://localhost:%d/data/%s", followerPort, key)
	resp, err := http.Get(readURL)
	if err != nil {
		t.Fatalf("Falha ao enviar GET para o seguidor: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Esperado status 200 para GET no seguidor, obtido %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)

	if result["value"] != value {
		t.Errorf("Valor lido no seguidor incorreto. Esperado: %s, Obtido: %s", value, result["value"])
	}
	t.Logf("Teste de leitura do seguidor bem-sucedido.")
}

// Para executar os testes, primeiro você deve iniciar o cluster:
// 1. go run main.go --id=node1 --port=8080 --raft-port=7000 --bootstrap
// 2. go run main.go --id=node2 --port=8081 --raft-port=7001 --peers="127.0.0.1:7000"
// 3. go run main.go --id=node3 --port=8082 --raft-port=7002 --peers="127.0.0.1:7000"
// 4. go test -v