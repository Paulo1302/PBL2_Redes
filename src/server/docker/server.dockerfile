# ----------------------------------------------------
# STAGE 1: BUILDER
# ----------------------------------------------------
FROM golang:1.24.5 AS builder
WORKDIR /app
COPY go.mod ./
COPY go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server_app .

# ----------------------------------------------------
# STAGE 2: FINAL
# ----------------------------------------------------
FROM alpine:latest

# --- ALTERAÇÃO 1: Instala iproute2 (para descoberta de IP) ---
RUN apk --no-cache add ca-certificates iproute2

WORKDIR /app
COPY --from=builder /app/server_app .

# --- ALTERAÇÃO 2: Copia o entrypoint e o torna executável ---
COPY docker/entrypoint.sh .
RUN chmod +x entrypoint.sh

EXPOSE 8080 7000

# --- ALTERAÇÃO 3: Define o entrypoint ---
ENTRYPOINT ["./entrypoint.sh"]

# --- ALTERAÇÃO 4: Define o CMD padrão (para o Líder) ---
# Note que --raft-addr foi REMOVIDO daqui, pois o entrypoint vai adicioná-lo
CMD ["--id", "1", "--bootstrap", "true", "--port", "8080", "--raft-port", "7000"]