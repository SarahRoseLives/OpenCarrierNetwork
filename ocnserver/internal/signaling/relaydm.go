package signaling

import (
	"context"
	"fmt"
	"log"

	"github.com/open-carrier-network/ocn/internal/dm"
	"github.com/open-carrier-network/ocn/internal/numbers"
	ocnserverpb "github.com/open-carrier-network/ocn/proto/ocnserver"
)

// deliverInboundDM handles a message relayed from another server for one of
// our local users: enqueue it in their outbox and relay/push if reachable.
func (srv *Server) deliverInboundDM(env *dm.Envelope) error {
	if env == nil || env.To == "" {
		return fmt.Errorf("invalid dm envelope")
	}
	mgr := srv.dmManager()
	if mgr == nil {
		return fmt.Errorf("messaging unavailable")
	}
	area, local, err := numbers.ParseNumber(env.To, srv.area())
	if err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}
	if srv.area() != "" && area != srv.area() {
		return fmt.Errorf("recipient %s not hosted here", env.To)
	}
	callee, err := srv.store.GetUserByNumber(local)
	if err != nil {
		return err
	}
	if callee == nil {
		return fmt.Errorf("user not found")
	}

	if err := mgr.EnqueueRemote(env.MessageID, local, env); err != nil {
		return err
	}
	if srv.relayDMMessage(local, env) {
		log.Printf("dm: relayed inbound %s to online %s", env.MessageID, local)
	} else if callee.FCMToken != "" {
		p := srv.pushSender()
		if p != nil {
			if err := p.SendMessageNotification(callee.FCMToken, env.From, env.FromName); err != nil {
				log.Printf("dm: push failed for %s: %v", local, err)
			}
		}
	}
	return nil
}

func dmEnvelopeToProto(env *dm.Envelope) *ocnserverpb.DMEnvelope {
	p := &ocnserverpb.DMEnvelope{
		MessageId: env.MessageID,
		ClientId:  env.ClientID,
		From:      env.From,
		FromName:  env.FromName,
		To:        env.To,
		Kind:      env.Kind,
		Text:      env.Text,
		CreatedAt: env.CreatedAt,
	}
	if env.Image != nil {
		p.Image = &ocnserverpb.DMImage{
			Name: env.Image.Name,
			Mime: env.Image.Mime,
			B64:  env.Image.B64,
		}
	}
	return p
}

func dmEnvelopeFromProto(p *ocnserverpb.DMEnvelope) *dm.Envelope {
	if p == nil {
		return nil
	}
	env := &dm.Envelope{
		MessageID: p.GetMessageId(),
		ClientID:  p.GetClientId(),
		From:      p.GetFrom(),
		FromName:  p.GetFromName(),
		To:        p.GetTo(),
		Kind:      p.GetKind(),
		Text:      p.GetText(),
		CreatedAt: p.GetCreatedAt(),
	}
	if p.GetImage() != nil {
		env.Image = &dm.Image{
			Name: p.GetImage().GetName(),
			Mime: p.GetImage().GetMime(),
			B64:  p.GetImage().GetB64(),
		}
	}
	return env
}

// DeliverDM implements the federated direct-message RPC.
func (g *grpcBridge) DeliverDM(ctx context.Context, req *ocnserverpb.DMEnvelope) (*ocnserverpb.DMDeliveryResponse, error) {
	resp := &ocnserverpb.DMDeliveryResponse{}
	if err := g.srv.deliverInboundDM(dmEnvelopeFromProto(req)); err != nil {
		log.Printf("dm: inbound DeliverDM rejected: %v", err)
		resp.ErrorMessage = err.Error()
		return resp, nil
	}
	resp.Accepted = true
	return resp, nil
}
