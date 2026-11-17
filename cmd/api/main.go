package main

import (
	"log/slog"
	"os"

	"spartial.local/backend/internal/apiserver"
)

func main() {
	if err := apiserver.Run(); err != nil {
		slog.Error("startup_failed", "reason", err.Error())
		os.Exit(1)
	}
}
