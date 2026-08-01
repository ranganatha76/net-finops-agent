# Build context is repo root (see docker-compose).
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /net-finops-agent ./cmd/net-finops-agent

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget
COPY --from=build /net-finops-agent /usr/local/bin/net-finops-agent
EXPOSE 8080
ENTRYPOINT ["net-finops-agent"]
CMD ["serve", "--config", "/config/config.yaml", "--mode", "demo"]
