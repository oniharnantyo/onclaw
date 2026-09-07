// createdb ensures the target database exists (CREATE DATABASE IF NOT EXISTS
// equivalent for PostgreSQL) so the /v1 smoke can run against a fresh name.
// Usage: go run ./scripts/v1smoke/createdb -dsn <url-to-target-db>
package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

func main() {
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "target database DSN")
	flag.Parse()
	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "-dsn is required")
		os.Exit(2)
	}

	cfg, err := pgx.ParseConfig(*dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse dsn: %v\n", err)
		os.Exit(1)
	}
	target := cfg.Database

	adminCfg := cfg.Copy()
	adminCfg.Database = "postgres"
	conn, err := pgx.ConnectConfig(context.Background(), adminCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close(context.Background())

	var exists bool
	if err := conn.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, target).Scan(&exists); err != nil {
		fmt.Fprintf(os.Stderr, "query: %v\n", err)
		os.Exit(1)
	}
	if exists {
		fmt.Println("database exists:", target)
		return
	}
	// Identifier from a DSN is not user-controlled free text in the smoke; still quote defensively.
	quoted := strings.ReplaceAll(url.QueryEscape(target), "+", "")
	if _, err := conn.Exec(context.Background(),
		fmt.Sprintf(`CREATE DATABASE %s`, pgx.Identifier{target}.Sanitize())); err != nil {
		fmt.Fprintf(os.Stderr, "create database: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("created database:", quoted)
}
