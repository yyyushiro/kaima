FROM golang:1.26-alpine
WORKDIR /app

RUN go install github.com/air-verse/air@latest
RUN go install github.com/go-delve/delve/cmd/dlv@latest

COPY go.mod ./
RUN go mod download

COPY .air.toml ./
COPY . .

EXPOSE 8080
CMD ["air", "-c", ".air.toml"]
