package main

import (
	"log"
	"os"
	"path/filepath"

	"project-for-yingbai/backend/internal/router"
)

const defaultPort = "8080"

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	mobileDir := os.Getenv("MOBILE_DIR")
	if mobileDir == "" {
		mobileDir = filepath.Join("..", "mobile")
	}

	engine := router.New(mobileDir)
	address := "0.0.0.0:" + port
	log.Printf("backend listening on http://%s", address)

	if err := engine.Run(address); err != nil {
		log.Fatalf("backend stopped: %v", err)
	}
}
