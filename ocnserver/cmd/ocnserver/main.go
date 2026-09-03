package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/open-carrier-network/ocn/config"
	"github.com/open-carrier-network/ocn/internal/auth"
	"github.com/open-carrier-network/ocn/internal/fcm"
	"github.com/open-carrier-network/ocn/internal/ksim"
	"github.com/open-carrier-network/ocn/internal/numbers"
	"github.com/open-carrier-network/ocn/internal/services"
	"github.com/open-carrier-network/ocn/internal/signaling"
	"github.com/open-carrier-network/ocn/internal/store"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	fcmCreds := flag.String("fcm-creds", "", "path to Firebase service account JSON")
	flag.Parse()

	// Load config
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize store
	db, err := store.New(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Initialize auth manager
	authMgr := auth.NewAuthManager()

	// Initialize number allocator
	allocator := numbers.NewAllocator(cfg.AreaCode, db.NumberExists)

	// Initialize services
	serviceRegistry := services.NewRegistry()
	tts := services.NewTTS("tts_cache")
	echoService, err := services.NewEchoService(tts)
	if err != nil {
		log.Fatalf("Failed to create echo service: %v", err)
	}
	serviceRegistry.Register(echoService)

	// Initialize FCM client (optional)
	var fcmClient *fcm.Client
	if *fcmCreds != "" {
		fcmClient, err = fcm.NewClient(*fcmCreds)
		if err != nil {
			log.Printf("WARNING: FCM client failed to initialize: %v", err)
		}
	} else {
		log.Printf("FCM not configured (use --fcm-creds to enable push notifications)")
	}

	// Initialize signaling server
	sigServer := signaling.NewServer(db, authMgr, allocator, cfg.AreaCode, serviceRegistry, fcmClient)

	// Load or generate server keypair
	if _, err := os.Stat(cfg.ServerKeyPath); os.IsNotExist(err) {
		log.Printf("Generating new server keypair: %s", cfg.ServerKeyPath)
		k, err := ksim.Generate()
		if err != nil {
			log.Fatalf("Failed to generate server keypair: %v", err)
		}
		if err := k.SaveFile(cfg.ServerKeyPath, cfg.ServerName, cfg.ServerName, "", ""); err != nil {
			log.Fatalf("Failed to save server keypair: %v", err)
		}
	}

	// Set up HTTP routes
	mux := http.NewServeMux()

	// WebSocket signaling endpoint
	mux.HandleFunc("/ws", sigServer.HandleWebSocket)

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","area_code":"%s","server":"%s"}`, cfg.AreaCode, cfg.ServerName)
	})

	// Server info
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		count, _ := db.CountUsers()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"area_code":"%s","name":"%s","description":"%s","users":%d}`,
			cfg.AreaCode, cfg.ServerName, cfg.Description, count)
	})

	addr := cfg.Address()
	if cfg.AreaCode != "" {
		log.Printf("OCN Server starting on %s (area code: %s)", addr, cfg.AreaCode)
	} else {
		log.Printf("OCN Server starting on %s (standalone mode - no area code)", addr)
	}
	log.Printf("  WebSocket: ws://%s/ws", addr)
	log.Printf("  Health:    http://%s/health", addr)
	log.Printf("  Info:      http://%s/info", addr)
	log.Printf("  Services:  *01 Echo Test")
	if fcmClient != nil {
		log.Printf("  FCM:       enabled")
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		os.Exit(0)
	}()

	if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
		log.Printf("TLS enabled")
		log.Fatal(http.ListenAndServeTLS(addr, cfg.TLSCertFile, cfg.TLSKeyFile, mux))
	} else {
		log.Printf("WARNING: TLS not configured. Use --tls-cert and --tls-key for production.")
		log.Fatal(http.ListenAndServe(addr, mux))
	}
}
