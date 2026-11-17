package main

import (
	"errors"
	"log/slog"
	"os"

	"github.com/joho/godotenv"
	"spartial.local/backend/internal/apiserver"
)

func main() {
	// Explicit environment variables take precedence over local development values.
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Error("startup_failed", "reason", "could not load .env")
		os.Exit(1)
	}
	if err := apiserver.Run(); err != nil {
		slog.Error("startup_failed", "reason", err.Error())
		os.Exit(1)
	}
}
