package main

import (
	"context"
	"log"
	"net/http"
	"nexus/internal/api"
	"nexus/internal/dispatch"
	"nexus/internal/reconcile"
	"nexus/internal/store"
	"nexus/internal/supervisor"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := store.InitDB("nexus.db"); err != nil {
		log.Fatalf("Failed to init db: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	supervisor.DefaultSupervisor.Start(ctx, 3)
	go reconcile.StartReconciler(ctx)
	go dispatch.StartDrainer(ctx)

	router := api.NewRouter()

	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	go func() {
		log.Println("Starting platform on :8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")
	cancel() // kill all workers

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	store.DB.Close()
	log.Println("Shutdown complete")
}
