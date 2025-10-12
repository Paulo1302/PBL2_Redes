# Estágio 1: Builder - Compila a aplicação Go
# Usamos uma imagem oficial do Go com Alpine para um tamanho menor.
FROM golang:1.24-alpine AS builder

# Define o diretório de trabalho dentro do contentor.
WORKDIR /app

# Instala ferramentas básicas necessárias (git é necessário para o 'go mod download').
RUN apk --no-cache add ca-certificates git

# Copia os ficheiros do módulo primeiro para otimizar o cache do Docker.
# Se estes ficheiros não mudarem, o Docker não precisará de descarregar as dependências novamente.
COPY src/go.mod src/go.sum ./

# Descarrega todas as dependências do projeto.
RUN go mod download

# Agora, copia todo o código-fonte do projeto.
COPY src/ ./

# Compila a aplicação.
# - CGO_ENABLED=0: Desativa CGO para criar um binário estático.
# - GOOS=linux: Especifica que o binário deve ser compilado para Linux.
# - -o ./game-server: Define o nome do ficheiro de saída.
# - ./server: Especifica o pacote a ser compilado (a pasta que contém o main.go do servidor).
RUN CGO_ENABLED=0 GOOS=linux go build -o ./game-server ./server

# --- Estágio 2: Imagem Final ---
# Começa a partir de uma imagem Alpine limpa e mínima para a imagem final.
FROM alpine:latest

# Instala apenas as dependências de runtime necessárias.
RUN apk --no-cache add ca-certificates curl jq

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