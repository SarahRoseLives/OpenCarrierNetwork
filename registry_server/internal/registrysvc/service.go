package registrysvc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/open-carrier-network/ocn/registry/internal/push"
	"github.com/open-carrier-network/ocn/registry/internal/store"
	pb "github.com/open-carrier-network/ocn/registry/proto/registry"
)

// Server implements OCNRegistry.
type Server struct {
	pb.UnimplementedOCNRegistryServer
	store    *store.Store
	push     *push.Client
	stun     []string
	turnHost string
	turnUser string
	turnPass string
}

func New(st *store.Store, pusher *push.Client, stun []string) *Server {
	return &Server{store: st, push: pusher, stun: stun}
}

// SetTURN advertises the embedded TURN server in GetICECandidates.
func (s *Server) SetTURN(host, username, password string) {
	s.turnHost = host
	s.turnUser = username
	s.turnPass = password
}

func (s *Server) RegisterOCNServer(ctx context.Context, req *pb.RegisterOCNServerRequest) (*pb.RegisterOCNServerResponse, error) {
	if req.ServerAddress == "" {
		return nil, status.Error(codes.InvalidArgument, "server_address is required")
	}
	if len(req.OcnserverPublicKey) != 32 {
		return nil, status.Error(codes.InvalidArgument, "ocnserver_public_key must be a 32-byte Ed25519 key")
	}

	code, err := s.store.RegisterServer(&store.ServerInfo{
		AreaCode:    req.RequestedAreaCode,
		Name:        req.Name,
		Description: req.Description,
		ServerAddr:  req.ServerAddress,
		PublicKey:   req.OcnserverPublicKey,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrAreaInvalid), errors.Is(err, store.ErrAreaTaken):
			return &pb.RegisterOCNServerResponse{
				Success:      false,
				ErrorMessage: err.Error(),
			}, nil
		case errors.Is(err, store.ErrNoFreeAreas):
			return nil, status.Error(codes.ResourceExhausted, err.Error())
		default:
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	return &pb.RegisterOCNServerResponse{Success: true, AssignedAreaCode: code}, nil
}

func (s *Server) DeregisterOCNServer(ctx context.Context, req *pb.DeregisterOCNServerRequest) (*emptypb.Empty, error) {
	err := s.store.DeregisterServer(req.AreaCode, req.Timestamp, req.Signature)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrServerNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		case errors.Is(err, store.ErrBadSignature), errors.Is(err, store.ErrBadTimestamp):
			return nil, status.Error(codes.PermissionDenied, err.Error())
		default:
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	return &emptypb.Empty{}, nil
}

func (s *Server) ListOCNServers(ctx context.Context, req *pb.ListOCNServersRequest) (*pb.ListOCNServersResponse, error) {
	list, err := s.store.ListServers(int(req.PageSize))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out := &pb.ListOCNServersResponse{TotalCount: int32(len(list))}
	for _, sv := range list {
		out.Ocnservers = append(out.Ocnservers, &pb.OCNServerInfo{
			AreaCode:           sv.AreaCode,
			Name:               sv.Name,
			Description:        sv.Description,
			ServerAddress:      sv.ServerAddr,
			OcnserverPublicKey: sv.PublicKey,
			RegisteredAt:       sv.RegisteredAt.Unix(),
			Status:             sv.Status,
		})
	}
	return out, nil
}

func (s *Server) GetRoute(ctx context.Context, req *pb.GetRouteRequest) (*pb.GetRouteResponse, error) {
	info, err := s.store.GetRoute(req.AreaCode)
	if err != nil {
		return &pb.GetRouteResponse{Found: false}, nil
	}
	return &pb.GetRouteResponse{
		Found: true,
		Ocnserver: &pb.OCNServerInfo{
			AreaCode:           info.AreaCode,
			Name:               info.Name,
			Description:        info.Description,
			ServerAddress:      info.ServerAddr,
			OcnserverPublicKey: info.PublicKey,
			RegisteredAt:       info.RegisteredAt.Unix(),
			Status:             info.Status,
		},
	}, nil
}

func (s *Server) ResolveService(ctx context.Context, req *pb.ResolveServiceRequest) (*pb.ResolveServiceResponse, error) {
	sn, host, err := s.store.ResolveService(req.FullNumber)
	if err != nil {
		return &pb.ResolveServiceResponse{Found: false, FullNumber: req.FullNumber}, nil
	}
	return &pb.ResolveServiceResponse{
		Found:       true,
		FullNumber:  sn.FullNumber,
		Vanity:      sn.Vanity,
		Name:        sn.Name,
		Description: sn.Description,
		Ocnserver: &pb.OCNServerInfo{
			AreaCode:           host.AreaCode,
			Name:               host.Name,
			Description:        host.Description,
			ServerAddress:      host.ServerAddr,
			OcnserverPublicKey: host.PublicKey,
			RegisteredAt:       host.RegisteredAt.Unix(),
			Status:             host.Status,
		},
	}, nil
}

func (s *Server) GetICECandidates(ctx context.Context, req *pb.ICECandidateRequest) (*pb.ICECandidateResponse, error) {
	resp := &pb.ICECandidateResponse{}

	// STUN servers.
	for _, c := range s.stun {
		resp.IceServers = append(resp.IceServers, &pb.ICEServer{
			Urls: []string{"stun:" + c},
		})
		if resp.ServerReflexiveAddress == "" {
			resp.ServerReflexiveAddress = c
		}
	}

	// Embedded TURN relay (registry).
	if s.turnHost != "" && s.turnUser != "" {
		resp.IceServers = append(resp.IceServers, &pb.ICEServer{
			Urls: []string{
				"turn:" + s.turnHost + "?transport=udp",
				"turn:" + s.turnHost + "?transport=tcp",
			},
			Username:   s.turnUser,
			Credential: s.turnPass,
		})
	}
	return resp, nil
}

func (s *Server) PushDevice(ctx context.Context, req *pb.PushDeviceRequest) (*emptypb.Empty, error) {
	if s.push == nil {
		return nil, status.Error(codes.Unavailable, "registry push not configured")
	}
	// Authenticate: the calling server must be registered and sign the request.
	if err := s.store.VerifyPushAuth(req.AreaCode, req.Timestamp, []byte(req.Token), req.Signature); err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if req.Token == "" || req.CallId == "" {
		return nil, status.Error(codes.InvalidArgument, "token and call_id are required")
	}
	if err := s.push.SendCallNotification(req.Token, req.CallId, req.CallerNumber, req.CallerName); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	return &emptypb.Empty{}, nil
}
