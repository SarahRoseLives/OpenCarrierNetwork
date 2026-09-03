package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/open-carrier-network/ocn/config"
	"github.com/open-carrier-network/ocn/internal/admin"
	"github.com/open-carrier-network/ocn/internal/auth"
	"github.com/open-carrier-network/ocn/internal/fcm"
	"github.com/open-carrier-network/ocn/internal/ksim"
	"github.com/open-carrier-network/ocn/internal/numbers"
	"github.com/open-carrier-network/ocn/internal/push"
	"github.com/open-carrier-network/ocn/internal/registry"
	"github.com/open-carrier-network/ocn/internal/services"
	"github.com/open-carrier-network/ocn/internal/signaling"
	"github.com/open-carrier-network/ocn/internal/store"
	ocnserverpb "github.com/open-carrier-network/ocn/proto/ocnserver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	fcmCreds := flag.String("fcm-creds", "", "path to Firebase service account JSON")
	// Federation overrides
	registryAddr := flag.String("registry-address", "", "OCN registry gRPC host:port (overrides config)")
	registryArea := flag.String("registry-area", "", "requested area code (overrides config)")
	registryInsecure := flag.Bool("registry-insecure", false, "plaintext to registry (dev only)")
	fedAddr := flag.String("fed-addr", "", "inter-server gRPC listen address (overrides config)")
	fedPublic := flag.String("fed-public", "", "reachable inter-server address advertised to registry")
	fedInsecure := flag.Bool("fed-insecure", false, "plaintext inter-server gRPC (dev only)")
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

	// Load or generate the server keypair (used to sign registry requests).
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
	srvKey, _, err := ksim.LoadFile(cfg.ServerKeyPath, "")
	if err != nil {
		log.Fatalf("Failed to load server keypair: %v", err)
	}

	// Initialize auth manager
	authMgr := auth.NewAuthManager()

	regAddress := *registryAddr
	if regAddress == "" {
		regAddress = cfg.RegistryAddress
	}
	reqArea := *registryArea
	if reqArea == "" {
		reqArea = cfg.RegistryAreaCode
	}
	regInsecure := *registryInsecure || cfg.RegistryInsecure
	fedPublicAddr := *fedPublic
	if fedPublicAddr == "" {
		fedPublicAddr = cfg.FedPublicAddr
	}

	// Federation settings saved via the admin panel fill any remaining gaps
	// (config file and CLI flags take precedence when set).
	if fs, err := db.GetFederationSettings(); err != nil {
		log.Fatalf("Reading federation settings: %v", err)
	} else if fs.RegistryAddress != "" {
		if regAddress == "" {
			regAddress = fs.RegistryAddress
		}
		if !*registryInsecure && !cfg.RegistryInsecure {
			regInsecure = fs.RegistryInsecure
		}
		if reqArea == "" {
			reqArea = fs.RequestedAreaCode
		}
		if fedPublicAddr == "" {
			fedPublicAddr = fs.FederationPublicAddr
		}
	}

	var regClient *registry.Client
	var areaCode string
	if regAddress != "" {
		regClient, err = registry.Dial(regAddress, regInsecure)
		if err != nil {
			log.Fatalf("Failed to connect to registry %s: %v", regAddress, err)
		}
		defer regClient.Close()
		regClient.SetIdentity(srvKey)

		// On restart, keep the area code we were already assigned.
		if stored, _ := db.GetSetting("area_code"); stored != "" {
			reqArea = stored
		}
		areaCode, err = regClient.RegisterServer(
			cfg.ServerName, cfg.Description, fedPublicAddr, reqArea, srvKey.PublicKey,
		)
		if err != nil {
			log.Fatalf("Registry registration failed: %v", err)
		}
		if err := db.SetSetting("area_code", areaCode); err != nil {
			log.Fatalf("Persisting area code: %v", err)
		}
		cfg.AreaCode = areaCode
		// Stamp existing local lines (created before federation) with the area
		// code so their numbers display as full numbers.
		if err := db.UpdateAreaCodeForUsers(areaCode); err != nil {
			log.Printf("WARNING: failed to backfill user area codes: %v", err)
		}
		log.Printf("Federated: assigned area code %s", areaCode)
	} else {
		areaCode = cfg.AreaCode
	}

	// Initialize number allocator with the effective area code
	allocator := numbers.NewAllocator(areaCode, db.NumberExists)

	// Initialize services
	serviceRegistry := services.NewRegistry()
	tts := services.NewTTS("tts_cache")
	echoService, err := services.NewEchoService(tts)
	if err != nil {
		log.Fatalf("Failed to create echo service: %v", err)
	}
	serviceRegistry.Register(echoService)

	// Initialize FCM push: local creds if given, otherwise registry push.
	var fcmClient *fcm.Client
	if *fcmCreds != "" {
		fcmClient, err = fcm.NewClient(*fcmCreds)
		if err != nil {
			log.Printf("WARNING: FCM client failed to initialize: %v", err)
		}
	}
	var pusher push.Sender
	switch {
	case fcmClient != nil:
		pusher = fcmClient
	case regClient != nil:
		regClient.SetArea(areaCode)
		pusher = regClient
	default:
		log.Printf("No push configured (offline callees will not ring)")
	}

	// Initialize signaling server
	sigServer := signaling.NewServer(db, authMgr, allocator, areaCode, serviceRegistry, pusher)
	if regClient != nil {
		sigServer.SetRegistry(regClient)
		// Fetch the registry's STUN/TURN servers to hand out to clients.
		if servers, err := regClient.ICEServers(context.Background()); err != nil {
			log.Printf("WARNING: failed to fetch ICE servers from registry: %v", err)
		} else if len(servers) > 0 {
			var ice []signaling.IceServer
			for _, s := range servers {
				ice = append(ice, signaling.IceServer{
					URLs:       s.URLs,
					Username:   s.Username,
					Credential: s.Credential,
				})
			}
			sigServer.SetICEServers(ice)
			log.Printf("Providing %d ICE server(s) to clients", len(ice))
		}
	}
	fedPlaintext := *fedInsecure || cfg.FedInsecure
	sigServer.SetFedInsecure(fedPlaintext)

	hasLocalPush := fcmClient != nil

	// Hot federation: called after a successful admin registration so the
	// running server joins immediately (no restart). Keeps the settings for the
	// next start too (handled by the admin endpoint).
	applyFederation := func(fs *store.FederationSettings, area string) error {
		regLive, err := registry.Dial(fs.RegistryAddress, fs.RegistryInsecure)
		if err != nil {
			return err
		}
		regLive.SetIdentity(srvKey)
		regLive.SetArea(area)
		sigServer.SetRegistry(regLive)
		if !hasLocalPush {
			sigServer.SetPusher(regLive)
		}
		sigServer.SetAreaCode(area)
		areaCode = area
		if err := db.UpdateAreaCodeForUsers(area); err != nil {
			log.Printf("WARNING: failed to backfill user area codes: %v", err)
		}
		log.Printf("Federation hot-applied: area code %s", area)
		return nil
	}

	// Admin web panel (provisioning + line management) on its own port
	adminSrv := admin.New(admin.Options{
		Store:         db,
		Online:        sigServer.OnlineNumbers,
		SignalingPort: cfg.Port,
		PublicAddress: cfg.PublicAddress,
		AreaCode:      areaCode,
		Area:          func() string { return areaCode },
		ServerName:    cfg.ServerName,
		ServerKeyPath: cfg.ServerKeyPath,
		OnFederated:   applyFederation,
	})

	// Set up HTTP routes
	mux := http.NewServeMux()

	// WebSocket signaling endpoint
	mux.HandleFunc("/ws", sigServer.HandleWebSocket)

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","area_code":"%s","server":"%s"}`, areaCode, cfg.ServerName)
	})

	// Server info
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		count, _ := db.CountUsers()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"area_code":"%s","name":"%s","description":"%s","users":%d}`,
			areaCode, cfg.ServerName, cfg.Description, count)
	})

	addr := cfg.Address()
	log.Printf("OCN Server starting (area code: %q)", areaCode)
	log.Printf("  WebSocket: ws://%s/ws", addr)
	log.Printf("  Health:    http://%s/health", addr)
	log.Printf("  Admin:     http://%s/  (default login admin/admin)", cfg.AdminAddress())
	log.Printf("  Services:  *01 Echo Test")
	if pusher != nil {
		log.Printf("  Push:      enabled (%T)", pusher)
	}

	// Start inter-server gRPC (bridge) listener.
	grpcListen := *fedAddr
	if grpcListen == "" {
		grpcListen = cfg.FedAddr
	}
	go startFederationGRPC(grpcListen, sigServer, fedPlaintext, cfg)

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		os.Exit(0)
	}()

	// Start admin web panel in its own goroutine
	go func() {
		log.Printf("Admin panel listening on %s", cfg.AdminAddress())
		if err := http.ListenAndServe(cfg.AdminAddress(), adminSrv.Handler()); err != nil {
			log.Fatalf("Admin panel failed: %v", err)
		}
	}()

	if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
		log.Printf("TLS enabled")
		log.Fatal(http.ListenAndServeTLS(addr, cfg.TLSCertFile, cfg.TLSKeyFile, mux))
	} else {
		log.Printf("WARNING: TLS not configured. Use --tls-cert and --tls-key for production.")
		log.Fatal(http.ListenAndServe(addr, mux))
	}
}

// startFederationGRPC serves the OCNServerService (cross-server BridgeCall).
func startFederationGRPC(listen string, srv *signaling.Server, plaintext bool, cfg *config.Config) {
	if listen == "" {
		return
	}
	var opts []grpc.ServerOption
	if plaintext {
		opts = append(opts)
	} else {
		if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
			log.Printf("WARNING: federation TLS requested but no cert configured; refusing to start on %s", listen)
			return
		}
		cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			log.Printf("WARNING: federation TLS cert error: %v", err)
			return
		}
		opts = append(opts, grpc.Creds(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		})))
	}

	lis, err := net.Listen("tcp", listen)
	if err != nil {
		log.Printf("WARNING: federation gRPC listen %s failed: %v", listen, err)
		return
	}
	grpcSrv := grpc.NewServer(opts...)
	ocnserverpb.RegisterOCNServerServiceServer(grpcSrv, signaling.NewGRPCBridge(srv))
	log.Printf("Federation gRPC (bridge) listening on %s", listen)
	go func() {
		_ = grpcSrv.Serve(lis)
	}()
}
