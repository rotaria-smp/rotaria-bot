# rotaria-bot

## Overview

This bot exposes a WebSocket endpoint that the Minecraft Forge mod connects to in order to exchange control commands and server/player events. The Go side uses [`mcbridge.Bridge`](internal/mcbridge/mcbridge.go) to track a single active Minecraft connection and multiplex request/response pairs plus asynchronous events.

- Attach endpoint (Minecraft mod connects here): `ws://<host>:<port>/mc`
- Generic broadcast endpoint (optional external listeners): `ws://<host>:<port>/ws`
- Bridge code: [`mcbridge.Bridge`](internal/mcbridge/mcbridge.go)
- WebSocket server wiring: [`websocket.Server`](internal/websocket/server.go)

## Frame Protocol

JSON, one object per WebSocket message.

Types:

| Type | Direction | Purpose |
|------|-----------|---------|
| CMD  | Discord bot -> Minecraft | Execute a high-level or raw server command |
| RES  | Minecraft -> Discord bot | Successful response to a CMD (matches `id`) |
| ERR  | Minecraft -> Discord bot | Error response to a CMD (matches `id`) |
| EVT  | Minecraft -> Discord bot | Asynchronous event (join/leave/status/chat/etc.) |

Common fields:

```
{
  "type": "CMD" | "RES" | "ERR" | "EVT",
  "id":   "<string>",        // only CMD/RES/ERR
  "body": "<string>",        // command body or response payload
  "msg":  "<string>",        // error message (ERR only)
  "topic":"<string>"         // event topic (EVT only)
}
```

The Go bridge sends `{"type":"CMD","id":"<id>","body":"<command>"}` and waits for either `RES` (success) or `ERR` (failure). Events (`EVT`) are forwarded to the registered callback supplied to `mcbridge.New`.

## Command Set (handled in the Minecraft mod)

The Java mod (DiscordBridge.onCommand) recognizes the following command prefixes in `body`:

- `whitelist add <player>` → Add player to whitelist
- `unwhitelist <player>` → Remove player from whitelist
- `kick <player>` → Kick player (used for blacklist enforcement)
- `say <message>` → Broadcast system chat message
- `commandexec <raw>` → Execute a raw server command (captures first line of output)
- Anything else → Broadcast as chat line
- (Internal) `list` (example shown in Go use) can be implemented via `commandexec list` if needed.

Responses:
- Success: `{"type":"RES","id":"<id>","body":"ok"}` or command output text
- Failure: `{"type":"ERR","id":"<id>","msg":"<error>"}`

## Events

Forge mod pushes events with topics (examples from existing Java code):

- `join` / `leave` (player lifecycle)
- `chat` (formatted `<Player> message`)
- `status` (periodic TPS/player count summary `[UPDATE] TPS: ...`)
- `lifecycle` (server start/stop notifications)

Each arrives as: `{"type":"EVT","topic":"status","body":"<text>"}`

The Go side hooks events via the callback in `mcbridge.New(func(topic, body string){ ... })`.

## Go Usage

Sending a command from another package:

```go
ctx := context.Background()
out, err := bridge.SendCommand(ctx, "whitelist add ExamplePlayer")
if err != nil {
    log.Printf("command failed: %v", err)
} else {
    log.Printf("response: %s", out)
}
```

Link: [`mcbridge.Bridge.SendCommand`](internal/mcbridge/mcbridge.go)

Checking connection:

```go
if !bridge.IsConnected() {
    log.Println("Minecraft not connected")
}
```

Link: [`mcbridge.Bridge.IsConnected`](internal/mcbridge/mcbridge.go)

## Sample WebSocket Session (Manual Testing)

Using `wscat` (install with `npm i -g wscat`):

```
$ wscat -c ws://localhost:8080/mc
> {"type":"CMD","id":"1","body":"say Hello from test"}
< {"type":"RES","id":"1","body":"ok"}
< {"type":"EVT","topic":"chat","body":"<Server> Hello from test"}
```

## Migration Notes (Legacy TCP NDJSON → WebSocket)

Previously the mod used a raw TCP NDJSON stream (`DiscordBridge` with `ServerSocket`). The new approach replaces line-delimited JSON with framed WebSocket messages but retains identical logical frame shapes (type/id/body/topic/msg). The Java side must:

1. Replace blocking `ServerSocket` accept loop with a WebSocket client connecting to `/mc`.
2. On receive: parse JSON, dispatch like before.
3. On send (responses/events): write one JSON object per WebSocket message instead of appending `\n`.

Until Java is updated, only the Go side speaks WebSocket; the legacy TCP variant will not attach to `/mc`.

## Error Semantics

- Timeout (Go side): if no `RES`/`ERR` within 10s → returns `timeout` error.
- Connection absent → `minecraft not connected`.
- Java side may send `ERR` with meaningful message for invalid input (e.g. server not ready).

## Environment

Configure listen address via `WS_ADDR` (default `:8080`). The Minecraft mod must know host:port and connect to `/mc`.

## Extending

Add new command prefixes in the Java handler; they automatically become available via `SendCommand`. For richer responses, include structured JSON in `body` and parse on Go side.

## References

- Bridge implementation: [`internal/mcbridge/mcbridge.go`](internal/mcbridge/mcbridge.go)
  - [`mcbridge.Bridge`](internal/mcbridge/mcbridge.go)
  - [`mcbridge.Bridge.SendCommand`](internal/mcbridge/mcbridge.go)
  - [`mcbridge.Bridge.IsConnected`](internal/mcbridge/mcbridge.go)
- WebSocket server: [`internal/websocket/server.go`](internal/websocket/server.go)
