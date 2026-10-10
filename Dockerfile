FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/dom-backend ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/dom-backend /dom-backend
# публичные сертификаты Сбера для проверки сервера Sber API (SBER_CA_DIR=/etc/sber-ca)
COPY certs/sber /etc/sber-ca
EXPOSE 8080
ENTRYPOINT ["/dom-backend"]
