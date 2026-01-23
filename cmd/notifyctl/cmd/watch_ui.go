package cmd

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#3C3C3C")).
			Padding(0, 1)

	deliveredStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	failedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F87"))
	pendingStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700"))
	retryingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8C00"))
)

type eventMsg *notifyv1.DeliveryStatusEvent

type WatchModel struct {
	requestID string
	events    []*notifyv1.DeliveryStatusEvent
	width     int
	height    int
	quit      bool
}

func NewWatchModel(requestID string) *WatchModel {
	return &WatchModel{
		requestID: requestID,
		events:    make([]*notifyv1.DeliveryStatusEvent, 0),
	}
}

func (m *WatchModel) Init() tea.Cmd {
	return nil
}

func (m *WatchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quit = true
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case eventMsg:
		m.events = append(m.events, (*notifyv1.DeliveryStatusEvent)(msg))
		// Keep only the last N events that fit in the view
		maxEvents := m.height - 5
		if maxEvents > 0 && len(m.events) > maxEvents {
			m.events = m.events[len(m.events)-maxEvents:]
		}
	}
	return m, nil
}

func (m *WatchModel) View() string {
	if m.quit {
		return ""
	}

	var s strings.Builder

	s.WriteString(titleStyle.Render("NotifyCtl Watch"))
	s.WriteString(fmt.Sprintf(" - Request ID: %s\n\n", m.requestID))

	s.WriteString(headerStyle.Render(fmt.Sprintf("%-30s %-15s %-20s", "DESTINATION", "STATUS", "MESSAGE")))
	s.WriteString("\n")

	for _, e := range m.events {
		statusStr := e.Status.String()[16:] // Strip 'DELIVERY_STATUS_'
		var statusStyled string
		switch e.Status {
		case notifyv1.DeliveryStatus_DELIVERY_STATUS_DELIVERED:
			statusStyled = deliveredStyle.Render(statusStr)
		case notifyv1.DeliveryStatus_DELIVERY_STATUS_FAILED:
			statusStyled = failedStyle.Render(statusStr)
		case notifyv1.DeliveryStatus_DELIVERY_STATUS_PENDING:
			statusStyled = pendingStyle.Render(statusStr)
		case notifyv1.DeliveryStatus_DELIVERY_STATUS_RETRYING:
			statusStyled = retryingStyle.Render(statusStr)
		default:
			statusStyled = statusStr
		}

		line := fmt.Sprintf("%-30s %-15s %-20s",
			truncate(e.Destination, 29),
			statusStyled,
			truncate(e.Message, 40),
		)
		s.WriteString(line + "\n")
	}

	if len(m.events) == 0 {
		s.WriteString("\n  Waiting for events...\n")
	}

	s.WriteString("\n  (Press q to quit)")

	return s.String()
}

func runWatchUI(ctx context.Context, stream notifyv1.NotifyService_StreamDeliveryStatusClient) error {
	m := NewWatchModel(watchRequestID)
	p := tea.NewProgram(m)

	go func() {
		for {
			event, err := stream.Recv()
			if err != nil {
				return
			}
			p.Send(eventMsg(event))
		}
	}()

	_, err := p.Run()
	return err
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
