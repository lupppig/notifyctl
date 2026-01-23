package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/lupppig/notifyctl/internal/domain"
	"github.com/lupppig/notifyctl/internal/events"
	"github.com/lupppig/notifyctl/internal/security"
	"github.com/lupppig/notifyctl/internal/store"
	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
	"github.com/nats-io/nats.go"
)

type NotifyServer struct {
	notifyv1.UnimplementedNotifyServiceServer
	eventHub     *events.Hub
	serviceStore store.ServiceStore
	jobStore     store.NotificationJobStore
	nc           *nats.Conn
}

func NewNotifyServer(eventHub *events.Hub, serviceStore store.ServiceStore, jobStore store.NotificationJobStore, nc *nats.Conn) *NotifyServer {
	return &NotifyServer{
		eventHub:     eventHub,
		serviceStore: serviceStore,
		jobStore:     jobStore,
		nc:           nc,
	}
}

func (s *NotifyServer) RegisterService(ctx context.Context, req *notifyv1.RegisterServiceRequest) (*notifyv1.RegisterServiceResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name required")
	}

	rawKey, err := security.GenerateKey()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "generate key: %v", err)
	}

	svc := &domain.Service{
		ID:         uuid.New().String(),
		Name:       req.Name,
		WebhookURL: req.WebhookUrl,
		Secret:     req.Secret,
		APIKey:     security.HashKey(rawKey),
	}

	log.Printf("[%s] ACTION: Registering service | Name: %s", time.Now().Format(time.RFC3339), req.Name)
	if err := s.serviceStore.Create(ctx, svc); err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			log.Printf("[WARN] Service already exists: %s", req.Name)
			return nil, status.Errorf(codes.AlreadyExists, "service with name %q already exists", req.Name)
		}
		log.Printf("[ERROR] Failed to create service: %v", err)
		return nil, status.Errorf(codes.Internal, "create service: %v", err)
	}

	log.Printf("[%s] SUCCESS: Service registered | ID: %s", time.Now().Format(time.RFC3339), svc.ID)
	return &notifyv1.RegisterServiceResponse{
		ServiceId: svc.ID,
		ApiKey:    rawKey,
	}, nil
}

func (s *NotifyServer) ListServices(ctx context.Context, req *notifyv1.ListServicesRequest) (*notifyv1.ListServicesResponse, error) {
	log.Printf("[%s] ACTION: Listing services", time.Now().Format(time.RFC3339))
	services, err := s.serviceStore.List(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to list services: %v", err)
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

	log.Printf("[%s] SUCCESS: Listed %d services", time.Now().Format(time.RFC3339), len(infos))
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

func (s *NotifyServer) SendNotification(ctx context.Context, req *notifyv1.SendNotificationRequest) (*notifyv1.SendNotificationResponse, error) {
	if req.ServiceId == "" {
		return nil, status.Error(codes.InvalidArgument, "service_id required")
	}

	notificationID := uuid.New().String()

	payload := req.Payload
	if payload == nil {
		payload = []byte("{}")
	}

	job := &domain.NotificationJob{
		RequestID:  notificationID,
		ServiceID:  req.ServiceId,
		Payload:    payload,
		Status:     "ACCEPTED",
		RetryCount: 0,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	log.Printf("[%s] ACTION: Sending notification | Service: %s | Topic: %s", time.Now().Format(time.RFC3339), req.ServiceId, req.Topic)
	if err := s.jobStore.Create(ctx, job); err != nil {
		log.Printf("[ERROR] Failed to persist job: %v", err)
		return nil, status.Errorf(codes.Internal, "persist job: %v", err)
	}

	// Record stat
	if err := s.jobStore.IncrementStats(ctx, req.ServiceId, "ACCEPTED", time.Now()); err != nil {
		log.Printf("[ERROR] Failed to increment stats: %v", err)
	}

	// Publish to NATS
	data, err := json.Marshal(job)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal job: %v", err)
	}

	if err := s.nc.Publish("notifications.jobs", data); err != nil {
		log.Printf("[ERROR] Failed to publish to NATS: %v", err)
		return nil, status.Errorf(codes.Internal, "publish to nats: %v", err)
	}

	log.Printf("[%s] SUCCESS: Notification enqueued | ID: %s", time.Now().Format(time.RFC3339), notificationID)
	return &notifyv1.SendNotificationResponse{
		NotificationId: notificationID,
	}, nil
}

func (s *NotifyServer) DeleteService(ctx context.Context, req *notifyv1.DeleteServiceRequest) (*notifyv1.DeleteServiceResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id required")
	}

	if err := s.serviceStore.Delete(ctx, req.Id); err != nil {
		return nil, status.Errorf(codes.Internal, "delete service: %v", err)
	}

	return &notifyv1.DeleteServiceResponse{}, nil
}

func (s *NotifyServer) ListNotificationJobs(ctx context.Context, req *notifyv1.ListNotificationJobsRequest) (*notifyv1.ListNotificationJobsResponse, error) {
	log.Printf("[%s] ACTION: Listing notification jobs | Service: %s", time.Now().Format(time.RFC3339), req.ServiceId)
	jobs, err := s.jobStore.List(ctx, req.ServiceId)
	if err != nil {
		log.Printf("[ERROR] Failed to list jobs: %v", err)
		return nil, status.Errorf(codes.Internal, "list jobs: %v", err)
	}

	var protoJobs []*notifyv1.NotificationJob
	for _, job := range jobs {
		// Extract "event" from payload if possible
		var event string
		var payloadMap map[string]interface{}
		if err := json.Unmarshal(job.Payload, &payloadMap); err == nil {
			if e, ok := payloadMap["event"].(string); ok {
				event = e
			}
		}

		protoJobs = append(protoJobs, &notifyv1.NotificationJob{
			RequestId: job.RequestID,
			ServiceId: job.ServiceID,
			Event:     event,
			Status:    job.Status,
			CreatedAt: job.CreatedAt.Format(time.RFC3339),
		})
	}

	log.Printf("[%s] SUCCESS: Listed %d jobs", time.Now().Format(time.RFC3339), len(protoJobs))
	return &notifyv1.ListNotificationJobsResponse{
		Jobs: protoJobs,
	}, nil
}

func (s *NotifyServer) GetStats(ctx context.Context, req *notifyv1.GetStatsRequest) (*notifyv1.GetStatsResponse, error) {
	log.Printf("[%s] ACTION: Getting stats | Service: %s", time.Now().Format(time.RFC3339), req.ServiceId)
	stats, err := s.jobStore.GetStats(ctx, req.ServiceId)
	if err != nil {
		log.Printf("[ERROR] Failed to get stats: %v", err)
		return nil, status.Errorf(codes.Internal, "get stats: %v", err)
	}

	var entries []*notifyv1.StatEntry
	for status, count := range stats {
		entries = append(entries, &notifyv1.StatEntry{
			Status: status,
			Count:  count,
		})
	}

	return &notifyv1.GetStatsResponse{
		Stats: entries,
	}, nil
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
