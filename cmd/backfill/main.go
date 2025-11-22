package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type WLJsonEntry struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

func main() {
	dbPath := flag.String("db", "whitelist.db", "path to sqlite database")
	wlPath := flag.String("whitelist", "whitelist.json", "path to whitelist.json")
	flag.Parse()

	log.Printf("Using database: %s", *dbPath)
	log.Printf("Using whitelist: %s", *wlPath)

	entries, err := loadWhitelistJSON(*wlPath)
	if err != nil {
		log.Fatalf("load whitelist: %v", err)
	}
	log.Printf("Loaded %d entries from whitelist.json", len(entries))

	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		log.Printf("WARN: failed to set WAL mode: %v", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		log.Printf("WARN: failed to set busy_timeout: %v", err)
	}

	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Fatalf("begin tx: %v", err)
	}

	updated := 0
	skipped := 0

	for _, e := range entries {
		uuidNoHyphens := strings.ReplaceAll(e.UUID, "-", "")
		name := e.Name

		if strings.TrimSpace(name) == "" || strings.TrimSpace(uuidNoHyphens) == "" {
			log.Printf("SKIP: empty name/uuid in whitelist.json: %+v", e)
			skipped++
			continue
		}

		res, err := tx.ExecContext(
			ctx,
			`UPDATE whitelist
             SET minecraft_uuid = ?, username = ?
             WHERE username = ?`,
			uuidNoHyphens, name, name,
		)
		if err != nil {
			_ = tx.Rollback()
			log.Fatalf("update failed for %s (%s): %v", name, uuidNoHyphens, err)
		}

		rows, err := res.RowsAffected()
		if err != nil {
			_ = tx.Rollback()
			log.Fatalf("rows affected for %s: %v", name, err)
		}

		if rows == 0 {
			log.Printf("INFO: no sqlite row for name=%q (uuid=%s) – skipped", name, uuidNoHyphens)
			skipped++
			continue
		}

		log.Printf("Updated name=%q -> uuid=%s (rows=%d)", name, uuidNoHyphens, rows)
		updated++

		time.Sleep(10 * time.Millisecond)
	}

	if err := tx.Commit(); err != nil {
		log.Fatalf("commit tx: %v", err)
	}

	log.Printf("Done. Updated %d entries, skipped %d.", updated, skipped)
}

func loadWhitelistJSON(path string) ([]WLJsonEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var entries []WLJsonEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return entries, nil
}
