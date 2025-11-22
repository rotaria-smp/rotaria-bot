package whitelist

import (
	"context"
	"database/sql"
	"errors"

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

func (s *Store) GetByUUID(ctx context.Context, uuid string) (*Entry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, discord_id, minecraft_uuid, username FROM whitelist WHERE minecraft_uuid=?`,
		uuid,
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

func (s *Store) UpdateUsernameByUUID(ctx context.Context, uuid, username string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE whitelist SET username=? WHERE minecraft_uuid=?`,
		username, uuid,
	)
	return err
}

func (s *Store) GetByUsername(ctx context.Context, username string) (*Entry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, discord_id, minecraft_uuid, username FROM whitelist WHERE username=?`,
		username,
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

func (s *Store) UpdateUsername(ctx context.Context, discordID, username string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE whitelist SET username=? WHERE discord_id=?`, username, discordID)
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
