// Package turn starts the registry's embedded TURN relay server.
package turn

import (
	"fmt"
	"log"
	"net"

	turn "github.com/pion/turn/v2"
)

// Options configures the embedded TURN server.
type Options struct {
	PublicIP string // public IP advertised in relay allocations (required)
	UDPAddr  string // e.g. ":3478"
	TCPAddr  string // e.g. ":3478" ("" disables TCP)
	Realm    string
	Username string
	Password string
}

// Start launches the TURN server. Returns a close func.
func Start(o Options) (func() error, error) {
	if o.PublicIP == "" {
		return nil, fmt.Errorf("turn public ip required")
	}
	if o.Realm == "" {
		o.Realm = "ocn"
	}
	if o.Username == "" || o.Password == "" {
		return nil, fmt.Errorf("turn username/password required")
	}
	pub := net.ParseIP(o.PublicIP)
	if pub == nil {
		return nil, fmt.Errorf("invalid public ip %q", o.PublicIP)
	}

	auth := func(username, realm string, _ net.Addr) ([]byte, bool) {
		if username == o.Username {
			return turn.GenerateAuthKey(o.Username, realm, o.Password), true
		}
		return nil, false
	}

	cfg := turn.ServerConfig{
		Realm:       o.Realm,
		AuthHandler: auth,
	}
	relay := &turn.RelayAddressGeneratorStatic{
		RelayAddress: pub,
		Address:      "0.0.0.0",
	}

	udpConn, err := net.ListenPacket("udp4", o.UDPAddr)
	if err != nil {
		return nil, fmt.Errorf("turn udp listen: %w", err)
	}
	cfg.PacketConnConfigs = append(cfg.PacketConnConfigs, turn.PacketConnConfig{
		PacketConn:            udpConn,
		RelayAddressGenerator: relay,
	})
	log.Printf("TURN udp listening on %s (realm=%s)", o.UDPAddr, o.Realm)

	if o.TCPAddr != "" {
		tcpLn, err := net.Listen("tcp4", o.TCPAddr)
		if err != nil {
			return nil, fmt.Errorf("turn tcp listen: %w", err)
		}
		cfg.ListenerConfigs = append(cfg.ListenerConfigs, turn.ListenerConfig{
			Listener:              tcpLn,
			RelayAddressGenerator: relay,
		})
		log.Printf("TURN tcp listening on %s", o.TCPAddr)
	}

	srv, err := turn.NewServer(cfg)
	if err != nil {
		return nil, fmt.Errorf("start turn server: %w", err)
	}
	log.Printf("TURN server started (public ip %s, user %q)", o.PublicIP, o.Username)
	return srv.Close, nil
}
