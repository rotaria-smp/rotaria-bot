# rotaria-bot

Minimal bridge between a Discord bot and a Minecraft NeoForge server over WebSocket.

## Architecture

- WebSocket endpoint: `ws://<host>:<port>/mc` handled in [`websocket.Server`](internal/websocket/server.go).
- Single in‑process bridge: [`mcbridge.Bridge`](internal/mcbridge/mcbridge.go) (methods: [`mcbridge.Bridge.SendCommand`](internal/mcbridge/mcbridge.go), [`mcbridge.Bridge.IsConnected`](internal/mcbridge/mcbridge.go), [`mcbridge.Bridge.SetHandler`](internal/mcbridge/mcbridge.go)).
- Discord application wrapper + slash commands, modals, and event forwarding: [`discord.App`](internal/discord/commands.go).
- Configuration + .env loading: [`config.Load`](internal/shared/config/config.go).
- Structured logging (file + optional console): [`logging.BootstrapFromEnv`](internal/shared/logging/logging.go).

## Frame Protocol (WebSocket /mc)

JSON object per message.

Fields:
- `type`: `CMD` | `RES` | `ERR` | `EVT`
- `id`: command correlation (CMD/RES/ERR)
- `body`: command payload or response/event text
- `msg`: error text (ERR)
- `topic`: event topic (EVT)

Examples:
- Command request: `{"type":"CMD","id":"<uuid>","body":"list"}`
- Success: `{"type":"RES","id":"<uuid>","body":"Online players: 3"}`
- Error: `{"type":"ERR","id":"<uuid>","msg":"timeout"}`
- Event: `{"type":"EVT","topic":"chat","body":"<Player> hello"}`

## Supported Command Inputs (Minecraft side)

Handled inside the Java mod’s bridge:

- `whitelist add <player>`
- `unwhitelist <player>`
- `kick <player>`
- `say <message>`
- `commandexec <raw>` (returns first line of output)
- `list` (maps to server command `list`)
- Otherwise: broadcast as chat system message

## Events (Minecraft → Discord)

Topic mapping (all via `EVT` frames):
- `chat`: `<Name> message`
- `join`: `**Name** joined the server.`
- `leave`: `**Name** left the server.`
- `lifecycle`: server start/stop notices
- `status`: server status or tick metrics

Forwarding rules (in [`discord.App.HandleMCEvent`](internal/discord/commands.go)):
- `chat`: avatar via `https://minotar.net/avatar/<username>` (webhook)
- `join` / `leave` / `lifecycle`: webhook simple text
- `status`: optional channel (`ServerStatusChannelID`)

## Discord → Minecraft Message Relay

Messages posted in configured channel `MinecraftDiscordMessengerChannelID` are sent to Minecraft as a plain broadcast: `[Discord] Username: message`. Blacklist terms are filtered before relay.

## Whitelist Workflow

1. `/whitelist` opens modal (username, age, plan).
2. Request posted to staff review channel (`WHITELIST_REQUESTS_CHANNEL_ID`) with Approve / Reject buttons.
3. Approval:
   - Stored via [`whitelist.Store.Add`](internal/whitelist/storage.go)
   - Command `whitelist add <username>` sent through bridge
   - Optional DM to applicant
4. Rejection: embed updated only.

## Report Workflow

`/report` modal collects type, player (optional), reason, evidence, context. Embed sent to moderation channel (`REPORT_CHANNEL_ID`) with Resolve / Dismiss buttons (action handlers in [`discord.App`](internal/discord/commands.go)).

## Slash Command Response Strategy

Long‑running bridge calls (`/list`, others that invoke Minecraft) use deferred ephemeral response first, then edit reply when the RES/ERR frame arrives. Prevents Discord 3s “application did not respond” timeout.

## Configuration (.env)

Defaults supplied in [`config.Load`](internal/shared/config/config.go). Variables:

- `DISCORD_TOKEN`
- `WS_ADDR` (default `:8080`)
- `DB_PATH` (e.g. `./dev.db` or `/data/rotaria.db`)
- `DB_STRICT` (`1` to fail instead of memory fallback)
- `BLACKLIST_PATH`
- `DISCORD_WEBHOOK_URL`
- `REPORT_CHANNEL_ID`
- `WHITELIST_REQUESTS_CHANNEL_ID`
- `MinecraftDiscordMessengerChannelID`
- `ServerStatusChannelID`
- `GUILD_ID`
- `MEMBER_ROLE_ID`
- Logging: `LOG_PATH`, `LOG_LEVEL` (`debug|info|warn|error`), `ENV=dev` enables console, or `LOG_CONSOLE=1`

`.env` auto‑loaded by [`godotenv`](internal/shared/config/config.go). Missing file is ignored.

## Logging

Implemented with slog + lumberjack rotation: [`logging.BootstrapFromEnv`](internal/shared/logging/logging.go).

- File: JSON at `LOG_PATH` (default `./logs/rotaria.log`)
- Console: enabled when `ENV=dev` or `LOG_CONSOLE=1`

## Persistence / SQLite

Whitelist entries stored in SQLite (`modernc.org/sqlite`) via [`whitelist.Open`](internal/whitelist/storage.go). If file path fails and `DB_STRICT!=1`, falls back to in‑memory (non‑persistent) DB for development.

## Building / Running

Local:
```sh
go run ./cmd/bot
```

Docker:
```sh
docker build -t rotaria-bot .
docker run --rm -e DISCORD_TOKEN=XXXX -p 8080:8080 rotaria-bot
```

Compose (service `bot`) reads env vars and links to Minecraft container (see `docker-compose.yml`).

## WebSocket Testing

Use `wscat`:
```sh
wscat -c ws://localhost:8080/mc
> {"type":"CMD","id":"test-1","body":"list"}
< {"type":"RES","id":"test-1","body":"Online players: ..."}
```

## Extension Points

- Add new Minecraft command prefixes in Java bridge; they immediately become callable via [`mcbridge.Bridge.SendCommand`](internal/mcbridge/mcbridge.go).
- Add new event topics; handle in [`discord.App.HandleMCEvent`](internal/discord/commands.go).
- Additional Discord commands: extend registration in `Register()` and implement deferred response pattern.

## Error Handling

Client command timeout (10s) → returns `timeout` error.
If bridge disconnected → `minecraft not connected`.
Missing command ID in RES/ERR logged at debug/error for diagnosis.

## Key Files

- Bridge: [`internal/mcbridge/mcbridge.go`](internal/mcbridge/mcbridge.go)
- WebSocket server: [`internal/websocket/server.go`](internal/websocket/server.go)
- Discord app: [`internal/discord/commands.go`](internal/discord/commands.go)
- Bot session wrapper: [`internal/discord/bot.go`](internal/discord/bot.go)
- Config: [`internal/shared/config/config.go`](internal/shared/config/config.go)
- Logging: [`internal/shared/logging/logging.go`](internal/shared/logging/logging.go)
- Whitelist store: [`internal/whitelist/storage.go`](internal/whitelist/storage.go)

## Security Notes

- Keep `DISCORD_TOKEN` and webhook URL out of version control.
- Validate usernames before whitelist approval.
- Blacklist enforcement kicks offending player via bridge command.

## Future

- Replace first‑line response limitation for `commandexec` with multi‑line aggregation.
- Add metrics (pending command count, latency) via status events.
- Optional authentication layer on `/mc` (token or HMAC).
