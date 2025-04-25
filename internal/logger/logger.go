package logger

import (
	"fmt"
	"log"
	"os"
)

var (
	Info  *log.Logger
	Error *log.Logger
	Debug *log.Logger
	Warn  *log.Logger
)

func Init() {
	Info = log.New(os.Stdout, "ℹ️  [INFO]  ", log.Ldate|log.Ltime|log.Lshortfile)
	Error = log.New(os.Stderr, "❌ [ERROR] ", log.Ldate|log.Ltime|log.Lshortfile)
	Debug = log.New(os.Stdout, "🐛 [DEBUG] ", log.Ldate|log.Ltime|log.Lshortfile)
	Warn = log.New(os.Stdout, "⚠️  [WARN]  ", log.LstdFlags|log.Lshortfile)
}

func PrintEnvSummary() {
	fmt.Println("🔍 Loaded Environment Variables:")
	fmt.Println("  GCP_PROJECT_ID =", os.Getenv("GCP_PROJECT_ID"))
	fmt.Println("  PUBSUB_TOPIC_NAME =", os.Getenv("PUBSUB_TOPIC_NAME"))
	fmt.Println("  EMAIL_RESPONSE_ADDRESS =", os.Getenv("EMAIL_RESPONSE_ADDRESS"))
}
