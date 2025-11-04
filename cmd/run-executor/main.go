package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/steveyegge/vc/internal/executor"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/storage/beads"
)

func main() {
	ctx := context.Background()

	// Discover database
	dbPath, err := storage.DiscoverDatabase()
	if err != nil {
		log.Fatalf("Failed to discover database: %v", err)
	}

	fmt.Printf("Using database: %s\n", dbPath)

	// Open storage
	store, err := beads.NewVCStorage(ctx, dbPath)
	if err != nil {
		log.Fatalf("Failed to open storage: %v", err)
	}
	defer store.Close()

	// Create executor with default config
	cfg := executor.DefaultConfig()
	cfg.Store = store
	cfg.Version = "dev"

	exec, err := executor.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create executor: %v", err)
	}

	// Start executor
	fmt.Println("Starting VC executor...")
	if err := exec.Start(ctx); err != nil {
		log.Fatalf("Executor failed: %v", err)
	}

	// Setup signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	fmt.Println("Executor running. Press Ctrl+C to stop.")

	// Wait for shutdown signal
	<-sigCh
	fmt.Println("\nShutting down executor...")

	if err := exec.Stop(ctx); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	fmt.Println("Executor stopped.")
}
