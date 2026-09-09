package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

func main() {
	action := "up"
	if len(os.Args) > 2 {
		fatal("usage: agentx-migrate [up|status|version|down]")
	}
	if len(os.Args) == 2 {
		action = os.Args[1]
	}
	if action != "up" && action != "status" && action != "version" && action != "down" {
		fatal("unknown command %q; use up, status, version, or down", action)
	}
	if action == "down" && os.Getenv("AGENTX_ALLOW_MIGRATION_DOWN") != "true" {
		fatal("down migrations require AGENTX_ALLOW_MIGRATION_DOWN=true and a verified database backup")
	}
	url := os.Getenv("AGENTX_DATABASE_URL")
	if url == "" {
		fatal("AGENTX_DATABASE_URL is required")
	}
	dir := os.Getenv("AGENTX_MIGRATIONS_DIR")
	if dir == "" {
		dir = "./migrations"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := sql.Open("pgx", url)
	if err != nil {
		fatal("open database: %v", err)
	}
	defer db.Close()
	if err = db.PingContext(ctx); err != nil {
		fatal("ping database: %v", err)
	}
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 300), lock.WithUnlockTimeout(1, 30))
	if err != nil {
		fatal("create migration lock: %v", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(dir), goose.WithSessionLocker(locker))
	if err != nil {
		fatal("load migrations: %v", err)
	}
	switch action {
	case "up":
		results, err := provider.Up(ctx)
		if err != nil {
			fatal("apply migrations: %v", err)
		}
		if len(results) == 0 {
			fmt.Println("database schema is current")
		}
		for _, result := range results {
			fmt.Println(result)
		}
	case "status":
		statuses, err := provider.Status(ctx)
		if err != nil {
			fatal("read migration status: %v", err)
		}
		for _, status := range statuses {
			fmt.Printf("%03d %-7s %s\n", status.Source.Version, status.State, status.Source.Path)
		}
	case "version":
		version, err := provider.GetDBVersion(ctx)
		if err != nil {
			fatal("read migration version: %v", err)
		}
		fmt.Println(version)
	case "down":
		result, err := provider.Down(ctx)
		if err != nil {
			fatal("roll back migration: %v", err)
		}
		fmt.Println(result)
	}
}
func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "migration error: "+format+"\n", args...)
	os.Exit(1)
}
