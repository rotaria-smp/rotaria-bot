FROM golang:1.24.5-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod go build -ldflags="-s -w" -o /out/rotaria-bot ./cmd/bot

# Runtime
FROM alpine:latest AS runner
WORKDIR /app
RUN apk add --no-cache ca-certificates

COPY --from=build /out/rotaria-bot /usr/local/bin/rotaria-bot
COPY blacklist.txt ./blacklist.txt

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/rotaria-bot"]
