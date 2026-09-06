package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	url := os.Getenv("AGENTX_DATABASE_URL")
	if url == "" {
		fatal("AGENTX_DATABASE_URL is required")
	}
	dir := os.Getenv("AGENTX_MIGRATIONS_DIR")
	if dir == "" {
		dir = "./migrations"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		fatal("open database: %v", err)
	}
	defer db.Close()
	if err = db.Ping(ctx); err != nil {
		fatal("ping database: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fatal("read migrations: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			fatal("read %s: %v", entry.Name(), err)
		}
		if _, err = db.Exec(ctx, string(raw)); err != nil {
			fatal("apply %s: %v", entry.Name(), err)
		}
		fmt.Printf("applied %s\n", entry.Name())
	}
}
func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "migration error: "+format+"\n", args...)
	os.Exit(1)
}
