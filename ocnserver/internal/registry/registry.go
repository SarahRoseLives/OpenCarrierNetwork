// Package registry is a gRPC client for the OCN registry: area-code
// assignment, routing, and delegated FCM push.
package registry

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/open-carrier-network/ocn/internal/ksim"
	registrypb "github.com/open-carrier-network/ocn/proto/registry"
)

// Client talks to the registry.
type Client struct {
	conn    *grpc.ClientConn
	pb      registrypb.OCNRegistryClient
	address string
	key     *ksim.KSim
	area    string
}

// Dial connects to the registry. addr is "host:port". When insecure is true
// (local/dev) TLS verification is skipped via plaintext transport.
func Dial(addr string, insecureConn bool) (*Client, error) {
	opts := []grpc.DialOption{
		grpc.WithUserAgent("ocn-server/1"),
	}
	if insecureConn {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(nil)))
	}
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial registry %s: %w", addr, err)
	}
	return &Client{
		conn:    conn,
		pb:      registrypb.NewOCNRegistryClient(conn),
		address: addr,
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// SetIdentity supplies the server's keypair used to authenticate pushes.
func (c *Client) SetIdentity(key *ksim.KSim) { c.key = key }

// SetArea records the area code assigned to this server (used to sign pushes).
func (c *Client) SetArea(area string) { c.area = area }

// RegisterServer asks the registry to assign an area code. requested may be
// empty for auto-assignment. The assigned code is returned.
func (c *Client) RegisterServer(name, description, serverAddress, requestedArea string, pub ed25519.PublicKey) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, err := c.pb.RegisterOCNServer(ctx, &registrypb.RegisterOCNServerRequest{
		Name:               name,
		Description:        description,
		ServerAddress:      serverAddress,
		RequestedAreaCode:  requestedArea,
		OcnserverPublicKey: pub,
	})
	if err != nil {
		return "", err
	}
	if !resp.Success {
		return "", fmt.Errorf("registry: %s", resp.ErrorMessage)
	}
	return resp.AssignedAreaCode, nil
}

// Route resolves an area code to a server gRPC address.
func (c *Client) Route(ctx context.Context, area string) (string, error) {
	resp, err := c.pb.GetRoute(ctx, &registrypb.GetRouteRequest{AreaCode: area})
	if err != nil {
		return "", err
	}
	if !resp.Found || resp.Ocnserver == nil {
		return "", fmt.Errorf("no route for area code %s", area)
	}
	return resp.Ocnserver.ServerAddress, nil
}

// IceServer mirrors the registry's ICE server description for WebRTC clients.
type IceServer struct {
	URLs       []string
	Username   string
	Credential string
}

// ICEServers returns the STUN/TURN servers clients should use (from registry).
func (c *Client) ICEServers(ctx context.Context) ([]IceServer, error) {
	resp, err := c.pb.GetICECandidates(ctx, &registrypb.ICECandidateRequest{})
	if err != nil {
		return nil, err
	}
	var out []IceServer
	for _, s := range resp.IceServers {
		out = append(out, IceServer{
			URLs:       s.Urls,
			Username:   s.Username,
			Credential: s.Credential,
		})
	}
	return out, nil
}

// SendCallNotification satisfies push.Sender by delegating to the registry's
// shared Firebase project. The request is authenticated with this server's key.
func (c *Client) SendCallNotification(token, callID, callerNumber, callerName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if c.area == "" || c.key == nil {
		return fmt.Errorf("registry push: server area/identity not set")
	}
	ts := time.Now().Unix()
	_, err := c.pb.PushDevice(ctx, &registrypb.PushDeviceRequest{
		Token:        token,
		CallId:       callID,
		CallerNumber: callerNumber,
		CallerName:   callerName,
		AreaCode:     c.area,
		Timestamp:    ts,
		Signature:    c.sign(ts),
	})
	if err != nil {
		if s, ok := status.FromError(err); ok {
			return fmt.Errorf("registry push: %s", s.Message())
		}
		return fmt.Errorf("registry push: %w", err)
	}
	return nil
}

// sign produces the auth signature over area|timestamp.
func (c *Client) sign(ts int64) []byte {
	if c.key == nil {
		return nil
	}
	data := []byte(fmt.Sprintf("%s|%d", c.area, ts))
	return c.key.Sign(data)
}
