// Package grpcapi exposes fleet.telemetry.v1.TelemetryIngest over gRPC.
package grpcapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	telemetryv1 "satfleet/ingest/internal/gen/telemetryv1"
	"satfleet/ingest/internal/ingest"
	"satfleet/ingest/internal/telemetry"
)

const maxRejectionSamples = 20

type Server struct {
	telemetryv1.UnimplementedTelemetryIngestServer
	svc *ingest.Service
}

// New builds a gRPC server with keepalives, optional bearer-token auth, health checks
// (for Kubernetes probes) and reflection (for grpcurl).
func New(svc *ingest.Service, token string) (*grpc.Server, *health.Server) {
	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: 30 * time.Second, Timeout: 10 * time.Second}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: 10 * time.Second, PermitWithoutStream: true}),
		grpc.MaxRecvMsgSize(4 << 20),
	}
	if token != "" {
		opts = append(opts,
			grpc.ChainUnaryInterceptor(unaryAuth(token)),
			grpc.ChainStreamInterceptor(streamAuth(token)),
		)
	}
	gs := grpc.NewServer(opts...)
	telemetryv1.RegisterTelemetryIngestServer(gs, &Server{svc: svc})
	hs := health.NewServer()
	healthpb.RegisterHealthServer(gs, hs)
	reflection.Register(gs)
	return gs, hs
}

// FromProto converts the wire message into the internal model.
func FromProto(p *telemetryv1.TelemetryReport) telemetry.Report {
	return telemetry.Report{
		TerminalID:      p.GetTerminalId(),
		Timestamp:       time.UnixMilli(p.GetTimestampMs()).UTC(),
		Seq:             p.GetSeq(),
		LinkState:       telemetry.LinkState(p.GetLinkState()),
		LatencyMs:       p.GetLatencyMs(),
		JitterMs:        p.GetJitterMs(),
		PacketLossPct:   p.GetPacketLossPct(),
		DownlinkMbps:    p.GetDownlinkMbps(),
		UplinkMbps:      p.GetUplinkMbps(),
		SnrDb:           p.GetSnrDb(),
		HasSnr:          true, // proto3 float has no presence; gRPC clients always send SNR
		ObstructionPct:  p.GetObstructionPct(),
		FirmwareVersion: p.GetFirmwareVersion(),
		Transport:       telemetry.TransportGRPC,
	}
}

func (s *Server) accept(ctx context.Context, msg *telemetryv1.TelemetryReport, ack *telemetryv1.IngestAck) error {
	r := FromProto(msg)
	reason, err := s.svc.Accept(ctx, &r)
	if err != nil {
		return toStatus(err)
	}
	if reason == "" {
		ack.Accepted++
		return nil
	}
	ack.Rejected++
	if len(ack.Rejections) < maxRejectionSamples {
		ack.Rejections = append(ack.Rejections, &telemetryv1.Rejection{TerminalId: msg.GetTerminalId(), Seq: msg.GetSeq(), Reason: string(reason)})
	}
	return nil
}

func (s *Server) StreamTelemetry(stream telemetryv1.TelemetryIngest_StreamTelemetryServer) error {
	m := s.svc.Metrics()
	m.GRPCStreams.Add(1)
	defer m.GRPCStreams.Add(-1)

	ack := &telemetryv1.IngestAck{}
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(ack)
		}
		if err != nil {
			return err
		}
		if err := s.accept(stream.Context(), msg, ack); err != nil {
			return err
		}
	}
}

func (s *Server) SendBatch(ctx context.Context, batch *telemetryv1.TelemetryBatch) (*telemetryv1.IngestAck, error) {
	ack := &telemetryv1.IngestAck{}
	for _, msg := range batch.GetReports() {
		if err := s.accept(ctx, msg, ack); err != nil {
			return nil, err
		}
	}
	return ack, nil
}

func toStatus(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

func authorized(ctx context.Context, token string) bool {
	md, _ := metadata.FromIncomingContext(ctx)
	for _, v := range md.Get("authorization") {
		if got := strings.TrimPrefix(v, "Bearer "); subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1 {
			return true
		}
	}
	return false
}

// Health checks stay open so probes work without credentials.
func isHealth(method string) bool { return strings.HasPrefix(method, "/grpc.health.v1.Health/") }

func unaryAuth(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		if !isHealth(info.FullMethod) && !authorized(ctx, token) {
			return nil, status.Error(codes.Unauthenticated, "missing or invalid bearer token")
		}
		return h(ctx, req)
	}
}

func streamAuth(token string) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		if !isHealth(info.FullMethod) && !strings.HasPrefix(info.FullMethod, "/grpc.reflection.") && !authorized(ss.Context(), token) {
			return status.Error(codes.Unauthenticated, "missing or invalid bearer token")
		}
		return h(srv, ss)
	}
}
