package server

import (
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/lupppig/notifyctl/internal/events"
	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

type NotifyServer struct {
	notifyv1.UnimplementedNotifyServiceServer
	eventHub *events.Hub
}

func NewNotifyServer(eventHub *events.Hub) *NotifyServer {
	return &NotifyServer{
		eventHub: eventHub,
	}
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
