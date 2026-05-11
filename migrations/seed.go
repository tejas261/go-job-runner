package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"errors"
	"path/filepath"
	"sort"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func Seed() {
	ctx := context.Background()

	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, falling back to system environment variables")
	}

	connStr := os.Getenv("DB_URL")
	if connStr == "" {
		log.Fatal("DB_URL is not set; set it in .env or your shell environment")
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	defer pool.Close()

	sqlFiles, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		log.Fatalf("Failed to list migration files: %v\n", err)
	}
	if len(sqlFiles) == 0 {
		log.Fatal("No .sql files found in migrations/")
	}

	sort.Strings(sqlFiles)

	for _, file := range sqlFiles {
		seedData, readErr := os.ReadFile(file)
		if readErr != nil {
			log.Fatalf("Error reading seed file %s: %v\n", file, readErr)
		}

		if _, execErr := pool.Exec(ctx, string(seedData)); execErr != nil {
			var pgErr *pgconn.PgError
			if errors.As(execErr, &pgErr) && pgErr.Code == "42P07" {
				fmt.Printf("Skipped %s (already exists)\n", file)
				continue
			}

			log.Fatalf("Seed execution failed for %s: %v\n", file, execErr)
		}

		fmt.Printf("Applied %s\n", file)
	}

	fmt.Println("Database seeded successfully from all SQL files!")
}

func Purge() {
	// Replace with your connection string
	godotenv.Load()
	connStr := os.Getenv("DB_URL")
	ctx := context.Background()

	// Create a connection pool
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	// SQL command to drop all tables in the 'public' schema
	dropTablesSQL := `
	DO $$ 
	DECLARE 
		r RECORD; 
	BEGIN 
		FOR r IN (SELECT tablename FROM pg_tables WHERE schemaname = 'public') LOOP 
			EXECUTE 'DROP TABLE IF EXISTS ' || quote_ident(r.tablename) || ' CASCADE'; 
		END LOOP; 
	END $$;
	`

	// Execute the command
	_, err = pool.Exec(ctx, dropTablesSQL)
	if err != nil {
		log.Fatalf("Error dropping tables: %v", err)
	}

	fmt.Println("All tables dropped successfully.")
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: go run migrations/*.go [seed|purge]")
	}
	switch os.Args[1] {
	case "seed":
		Seed()
	case "purge":
		Purge()
	}
}
