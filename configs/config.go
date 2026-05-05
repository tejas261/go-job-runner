package configs

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type EnvVars struct {
	DB_URL  string
	PORT    string
	WORKERS string
}

var Creds EnvVars

func Load() EnvVars {
	_ = godotenv.Load()
	DB_URL := os.Getenv("DB_URL")
	PORT := os.Getenv("SERVER_PORT")
	WORKERS := os.Getenv("WORKERS")

	if DB_URL == "" {

		log.Fatalf("DB_URL not configured")
	}
	if PORT == "" {
		log.Fatalf("PORT not configured")
	}
	if WORKERS == "" {
		log.Fatalf("WORKERS not configured")
	}
	Creds.DB_URL = DB_URL
	Creds.PORT = PORT
	Creds.WORKERS = WORKERS

	return Creds
}
