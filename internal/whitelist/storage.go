package whitelist

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/rotaria-smp/rotaria-bot/internal/discord/namemc"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
	_ "modernc.org/sqlite"
)

type Entry struct {
	ID            int64
	DiscordID     string
	Username      string
	MinecraftUUID string
}
type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		return nil, err
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS whitelist (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        discord_id TEXT NOT NULL UNIQUE,
				minecraft_uuid TEXT NOT NULL UNIQUE,
        username TEXT NOT NULL UNIQUE
    )`); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Add(ctx context.Context, discordID, minecraft_uuid, username string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO whitelist(discord_id,minecraft_uuid,username) VALUES(?,?,?)`, discordID, minecraft_uuid, username)
	return err
}

func (s *Store) UpdateUUID(ctx context.Context, discordID string, minecraft_uuid string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE whitelist SET minecraft_uuid=? WHERE discord_id=?`, minecraft_uuid, discordID)
	return err
}

func (s *Store) UpdateUser(ctx context.Context, discordID, minecraft_UUID, username string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE whitelist SET minecraft_uuid=?, username=? WHERE discord_id=?`, minecraft_UUID, username, discordID)
	return err
}

func (s *Store) GetByDiscord(ctx context.Context, discordID string) (*Entry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, discord_id, minecraft_uuid, username FROM whitelist WHERE discord_id=?`,
		discordID,
	)

	var e Entry
	if err := row.Scan(&e.ID, &e.DiscordID, &e.MinecraftUUID, &e.Username); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

func (s *Store) Remove(ctx context.Context, discordID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM whitelist WHERE discord_id=?`, discordID)
	return err
}

func (s *Store) BackfillUUIDsFromUsernames(ctx context.Context, c *namemc.Client) error {
	logging.L().Info("Backfill: starting")

	rows, err := s.db.QueryContext(ctx, `SELECT id, username FROM whitelist`)
	if err != nil {
		logging.L().Error("Backfill: query failed", "error", err)
		return err
	}
	defer rows.Close()

	type item struct {
		ID       int64
		Username string
	}
	var items []item

	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Username); err != nil {
			logging.L().Error("Backfill: scan failed", "error", err)
			return err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		logging.L().Error("Backfill: rows error", "error", err)
		return err
	}

	logging.L().Info("Backfill: loaded rows", "count", len(items))

	for _, it := range items {
		select {
		case <-ctx.Done():
			logging.L().Error("Backfill: context cancelled", "error", ctx.Err())
			return ctx.Err()
		default:
		}

		logging.L().Info("Backfill: processing row", "id", it.ID, "username", it.Username)

		uuid, err := c.UsernameToUUID(it.Username)
		if err != nil {
			logging.L().Error("Backfill: failed to lookup",
				"username", it.Username,
				"id", it.ID,
				"error", err,
			)
			continue
		}

		if _, err := s.db.ExecContext(
			ctx,
			`UPDATE whitelist SET minecraft_uuid=? WHERE id=?`,
			uuid, it.ID,
		); err != nil {
			logging.L().Error("Backfill: failed to update",
				"username", it.Username,
				"uuid", uuid,
				"id", it.ID,
				"error", err,
			)
			continue
		}

		logging.L().Info("Backfill: updated row", "id", it.ID, "username", it.Username, "uuid", uuid)

		time.Sleep(100 * time.Millisecond)
	}

	logging.L().Info("Backfill: finished", "rows", len(items))
	return nil
}
