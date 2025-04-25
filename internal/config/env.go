package config

import (
	"github.com/joho/godotenv"
	"log"
	"os"
)

func LoadEnv() {
	envFile := os.Getenv("ENV_FILE")
	if envFile == "" {
		envFile = ".env" // fallback if none specified
	}
	err := godotenv.Load(envFile)
	if err != nil {
		log.Fatal("❌ Error loading env file")
	}
}
