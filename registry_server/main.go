// OCN Registry server: area-code assignment, routing, push (FCM) and the
// opencarrier.network landing website, all in one Go process.
package main

import (
	"context"
	"crypto/tls"
	"embed"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/open-carrier-network/ocn/registry/internal/adminapi"
	"github.com/open-carrier-network/ocn/registry/internal/push"
	"github.com/open-carrier-network/ocn/registry/internal/registrysvc"
	"github.com/open-carrier-network/ocn/registry/internal/store"
	"github.com/open-carrier-network/ocn/registry/internal/turn"
	registrypb "github.com/open-carrier-network/ocn/registry/proto/registry"
)

//go:embed web
var webFS embed.FS

func main() {
	var (
		httpAddr  = flag.String("http-addr", ":80", "HTTP listen (redirect + ACME)")
		httpsAddr = flag.String("https-addr", ":443", "HTTPS listen (website)")
		grpcAddr  = flag.String("grpc-addr", ":7443", "gRPC listen (TLS)")
		domain    = flag.String("domain", "opencarrier.network", "primary domain")
		email     = flag.String("email", "sarahroselives@protonmail.com", "Let's Encrypt contact email")
		cacheDir  = flag.String("cache-dir", "", "autocert cache dir (default ./certs)")
		dbPath    = flag.String("db", "registry.db", "SQLite database path")
		fcmCreds  = flag.String("fcm-creds", "", "path to Firebase service account JSON (official shared project)")
		stunList  = flag.String("stun", "stun.l.google.com:19302", "comma-separated STUN list returned to clients")
		plaintext = flag.Bool("plaintext", false, "dev mode: no TLS/ACME")
		turnUDP   = flag.String("turn-udp", ":3478", "TURN UDP listen")
		turnTCP   = flag.String("turn-tcp", ":3478", "TURN TCP listen")
		turnIP    = flag.String("turn-public-ip", "", "public IP the TURN relay advertises (required to enable TURN)")
		turnUser  = flag.String("turn-username", "ocn", "TURN long-term credential username")
		turnPass  = flag.String("turn-password", "", "TURN long-term credential password")
		turnHost  = flag.String("turn-host", "", "advertised TURN host:port (default <domain>:3478)")
	)
	flag.Parse()

	if *cacheDir == "" {
		*cacheDir = filepath.Join("certs")
	}
	if err := os.MkdirAll(*cacheDir, 0700); err != nil {
		log.Fatalf("cache dir: %v", err)
	}

	// Store
	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	// Push (optional; needed for PushDevice)
	var pusher *push.Client
	if *fcmCreds != "" {
		pusher, err = push.NewClient(*fcmCreds)
		if err != nil {
			log.Fatalf("init push: %v", err)
		}
	} else {
		log.Printf("WARNING: --fcm-creds not set; PushDevice will be unavailable")
	}

	var stun []string
	for _, s := range strings.Split(*stunList, ",") {
		if t := strings.TrimSpace(s); t != "" {
			stun = append(stun, t)
		}
	}

	// Service
	svc := registrysvc.New(st, pusher, stun)
	adminHandler := adminapi.New(st).Handler()

	// Embedded TURN relay (enable by setting -turn-public-ip + -turn-password).
	if *turnIP != "" {
		turnClose, terr := turn.Start(turn.Options{
			PublicIP: *turnIP,
			UDPAddr:  *turnUDP,
			TCPAddr:  *turnTCP,
			Realm:    "ocn",
			Username: *turnUser,
			Password: *turnPass,
		})
		if terr != nil {
			log.Fatalf("TURN: %v", terr)
		}
		defer turnClose()
		advertised := *turnHost
		if advertised == "" {
			p := *turnUDP
			if strings.HasPrefix(p, ":") {
				p = p[1:]
			}
			advertised = fmt.Sprintf("%s:%s", *domain, p)
		}
		svc.SetTURN(advertised, *turnUser, *turnPass)
	} else {
		log.Printf("TURN disabled (set -turn-public-ip and -turn-password to enable)")
	}

	if *plaintext {
		// Dev mode: no TLS/ACME. Website on http-addr, gRPC plaintext.
		grpcSrv := grpc.NewServer()
		registrypb.RegisterOCNRegistryServer(grpcSrv, svc)
		gl, err := net.Listen("tcp", *grpcAddr)
		if err != nil {
			log.Fatalf("grpc listen %s: %v", *grpcAddr, err)
		}
		go func() { log.Fatalf("grpc: %v", grpcSrv.Serve(gl)) }()
		log.Printf("gRPC (plaintext) on %s; website on %s", *grpcAddr, *httpAddr)
		if err := http.ListenAndServe(*httpAddr, webMux(adminHandler)); err != nil {
			log.Fatalf("http: %v", err)
		}
		return
	}

	// TLS via Let's Encrypt (autocert). Shared by website + gRPC.
	hostPolicy := func(ctx context.Context, host string) error {
		if host == *domain || host == "www."+*domain {
			return nil
		}
		return fmt.Errorf("unrecognized host %q", host)
	}
	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: hostPolicy,
		Cache:      autocert.DirCache(*cacheDir),
		Email:      *email,
	}
	tlsCfg := &tls.Config{
		GetCertificate: m.GetCertificate,
		MinVersion:     tls.VersionTLS12,
		NextProtos:     []string{"h2", "http/1.1", acme.ALPNProto},
	}

	// --- gRPC (TLS via grpc creds; serve on the raw listener so TLS is only
	// applied once) ---
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsCfg)))
	registrypb.RegisterOCNRegistryServer(grpcServer, svc)
	lis, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		log.Fatalf("grpc listen %s: %v", *grpcAddr, err)
	}
	go func() {
		log.Printf("gRPC listening on %s (TLS)", *grpcAddr)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("grpc serve: %v", err)
		}
	}()

	// --- Website (HTTPS :443) ---
	webHandler := webMux(adminHandler)
	site := &http.Server{
		Addr:              *httpsAddr,
		Handler:           webHandler,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 15 * time.Second,
	}
	go func() {
		log.Printf("Website listening on %s (HTTPS/Let's Encrypt)", *httpsAddr)
		if err := site.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Fatalf("https serve: %v", err)
		}
	}()

	// --- HTTP :80 (ACME + redirect) ---
	httpSrv := &http.Server{
		Addr:              *httpAddr,
		Handler:           m.HTTPHandler(http.HandlerFunc(redirectHTTPS)),
		ReadHeaderTimeout: 15 * time.Second,
	}
	log.Printf("OCN Registry starting: https://%s/  gRPC %s", *domain, *grpcAddr)
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}

func redirectHTTPS(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if i := strings.Index(host, ":"); i >= 0 {
		host = host[:i]
	}
	http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusMovedPermanently)
}

func webMux(admin http.Handler) http.Handler {
	mux := http.NewServeMux()
	if admin != nil {
		mux.Handle("/admin/", admin)
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","service":"ocn-registry"}`)
	})
	index, _ := webFS.ReadFile("web/index.html")
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
	return mux
}
