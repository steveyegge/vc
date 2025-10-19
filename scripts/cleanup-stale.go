// scripts/cleanup-stale.go - Manual stale instance cleanup tool
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/vc/internal/storage"
)

func main() {
	ctx := context.Background()

	// Use default config to find database
	cfg := storage.DefaultConfig()

	// Allow override via environment variable
	if dbPath := os.Getenv("VC_DB_PATH"); dbPath != "" {
		cfg.Path = dbPath
	}

	fmt.Printf("Connecting to database: %s\n", cfg.Path)

	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening storage: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	// Run cleanup with 5 minute threshold (matching executor default)
	staleThresholdSecs := int((5 * time.Minute).Seconds())

	fmt.Printf("Running cleanup (stale threshold: %d seconds)...\n", staleThresholdSecs)

	cleaned, err := store.CleanupStaleInstances(ctx, staleThresholdSecs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during cleanup: %v\n", err)
		os.Exit(1)
	}

	if cleaned > 0 {
		fmt.Printf("✓ Cleaned up %d stale instance(s) and released their claims\n", cleaned)
	} else {
		fmt.Println("✓ No stale instances found")
	}
}
