package whitelist

import (
	"context"
	"database/sql"
	"errors"

	_ "modernc.org/sqlite"
)

type Entry struct {
	ID        int64
	DiscordID string
	Username  string
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// TODO : If user have changed their minecraft name this will be a problem we should save their minecraft UUID in db instead
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS whitelist (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        discord_id TEXT NOT NULL UNIQUE,
        username TEXT NOT NULL
    )`); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Add(ctx context.Context, discordID, username string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO whitelist(discord_id,username) VALUES(?,?)`, discordID, username)
	return err
}

func (s *Store) GetByDiscord(ctx context.Context, discordID string) (*Entry, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, discord_id, username FROM whitelist WHERE discord_id=?`, discordID)
	var e Entry
	if err := row.Scan(&e.ID, &e.DiscordID, &e.Username); err != nil {
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
