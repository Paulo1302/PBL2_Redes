FROM golang:1.21-alpine AS builder

WORKDIR /app

# Instalar dependências
RUN apk --no-cache add ca-certificates curl

# Copiar módulos
COPY src/cardGame/go.mod src/cardGame/go.sum ./
COPY src/cardGame/server/go.mod src/cardGame/server/go.sum ./server/

# Download dependencies
RUN go mod download
RUN cd server && go mod download

# Copiar código
COPY src/cardGame/ .

# Build
RUN cd server && CGO_ENABLED=0 GOOS=linux go build -o ../game-server .

FROM alpine:latest

RUN apk --no-cache add ca-certificates curl

WORKDIR /root/

COPY --from=builder /app/game-server .

EXPOSE 8080 7000

CMD ["./game-server"]
