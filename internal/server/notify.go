package server

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/lupppig/notifyctl/internal/domain"
	"github.com/lupppig/notifyctl/internal/events"
	"github.com/lupppig/notifyctl/internal/store"
	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

type NotifyServer struct {
	notifyv1.UnimplementedNotifyServiceServer
	eventHub     *events.Hub
	serviceStore store.ServiceStore
}

func NewNotifyServer(eventHub *events.Hub, serviceStore store.ServiceStore) *NotifyServer {
	return &NotifyServer{
		eventHub:     eventHub,
		serviceStore: serviceStore,
	}
}

func (s *NotifyServer) RegisterService(ctx context.Context, req *notifyv1.RegisterServiceRequest) (*notifyv1.RegisterServiceResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name required")
	}

	svc := &domain.Service{
		ID:         uuid.New().String(),
		Name:       req.Name,
		WebhookURL: req.WebhookUrl,
		Secret:     req.Secret,
		APIKey:     uuid.New().String(), // In a real app, this would be a secure token
	}

	if err := s.serviceStore.Create(ctx, svc); err != nil {
		return nil, status.Errorf(codes.Internal, "create service: %v", err)
	}

	return &notifyv1.RegisterServiceResponse{
		ServiceId: svc.ID,
		ApiKey:    svc.APIKey,
	}, nil
}

func (s *NotifyServer) ListServices(ctx context.Context, req *notifyv1.ListServicesRequest) (*notifyv1.ListServicesResponse, error) {
	services, err := s.serviceStore.List(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list services: %v", err)
	}

	var infos []*notifyv1.ServiceInfo
	for _, svc := range services {
		infos = append(infos, &notifyv1.ServiceInfo{
			Id:         svc.ID,
			Name:       svc.Name,
			WebhookUrl: svc.WebhookURL,
		})
	}

	return &notifyv1.ListServicesResponse{
		Services: infos,
	}, nil
}

func (s *NotifyServer) StreamDeliveryStatus(
	req *notifyv1.StreamDeliveryStatusRequest,
	stream notifyv1.NotifyService_StreamDeliveryStatusServer,
) error {
	if req.NotificationId == "" && req.ServiceId == "" {
		return status.Error(codes.InvalidArgument, "notification_id or service_id required")
	}

	sub := &events.Subscriber{
		ID:             uuid.New().String(),
		NotificationID: req.NotificationId,
		ServiceID:      req.ServiceId,
		Events:         make(chan events.DeliveryEvent, 100),
	}

	s.eventHub.Subscribe(sub)
	defer s.eventHub.Unsubscribe(sub.ID)

	for event := range sub.Events {
		resp := &notifyv1.DeliveryStatusEvent{
			NotificationId: event.NotificationID,
			Destination:    event.Destination,
			Status:         mapStatus(event.Status),
			Message:        event.Message,
			Attempt:        int32(event.Attempt),
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
	}

	return nil
}

func mapStatus(status events.DeliveryStatus) notifyv1.DeliveryStatus {
	switch status {
	case events.DeliveryStatusPending:
		return notifyv1.DeliveryStatus_DELIVERY_STATUS_PENDING
	case events.DeliveryStatusDelivering:
		return notifyv1.DeliveryStatus_DELIVERY_STATUS_DELIVERING
	case events.DeliveryStatusDelivered:
		return notifyv1.DeliveryStatus_DELIVERY_STATUS_DELIVERED
	case events.DeliveryStatusFailed:
		return notifyv1.DeliveryStatus_DELIVERY_STATUS_FAILED
	case events.DeliveryStatusRetrying:
		return notifyv1.DeliveryStatus_DELIVERY_STATUS_RETRYING
	default:
		return notifyv1.DeliveryStatus_DELIVERY_STATUS_UNSPECIFIED
	}
}
