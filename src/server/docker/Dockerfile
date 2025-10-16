# Estágio 1: Builder - Compila a aplicação Go
FROM golang:1.24-alpine AS builder

# Define o diretório de trabalho. Este é o local onde o binário será criado.
WORKDIR /app

# Instala ferramentas básicas necessárias
RUN apk update && apk --no-cache add ca-certificates git

# Copia os ficheiros do módulo primeiro. O contexto de build (a pasta src/server)
# contém go.mod e go.sum na raiz.
COPY go.mod go.sum ./

# Descarrega todas as dependências do projeto na raiz do WORKDIR (/app).
RUN go mod download

# Copia todo o código-fonte restante do contexto de build (API/, main.go, etc.)
COPY . .

# Compila a aplicação. Como o go.mod está em /app, a compilação é simples:
# go build . compila todos os arquivos do pacote atual (package server).
RUN CGO_ENABLED=0 GOOS=linux go build -o ./game-server .

# --- Estágio 2: Imagem Final ---
FROM alpine:latest

# Instala apenas as dependências de runtime necessárias.
# A linha foi corrigida para usar 'apk update' antes da instalação, se necessário.
RUN apk update && apk --no-cache add ca-certificates curl jq

# Cria um utilizador não-root para correr a aplicação por segurança.
RUN addgroup -g 1001 app && \
    adduser -D -s /bin/sh -u 1001 -G app app

# Define o diretório de trabalho na imagem final.
WORKDIR /app

# Copia apenas o binário compilado do estágio 'builder'.
COPY --from=builder /app/game-server .

# Altera a propriedade do diretório para o utilizador 'app'.
RUN chown -R app:app /app

# Muda para o utilizador não-root.
USER app

# Expõe as portas que a aplicação usa.
EXPOSE 8080 7000

# Define o comando padrão para executar quando o contentor iniciar.
CMD ["./game-server"]