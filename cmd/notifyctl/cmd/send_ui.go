package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

type stepStatus int

const (
	stepPending stepStatus = iota
	stepActive
	stepDone
	stepFailed
)

type step struct {
	label  string
	status stepStatus
}

type SendModel struct {
	steps    []step
	current  int
	err      error
	resultID string
	spinner  spinner.Model
	done     bool
	quitting bool

	// Payload for sending
	serviceID string
	topic     string
	payload   []byte
	dest      []*notifyv1.Destination
}

func NewSendModel(svcID, topic string, payload []byte, dest []*notifyv1.Destination) *SendModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return &SendModel{
		steps: []step{
			{label: "Validating payload"},
			{label: "Connecting to server"},
			{label: "Sending notification"},
		},
		spinner:   s,
		serviceID: svcID,
		topic:     topic,
		payload:   payload,
		dest:      dest,
	}
}

type nextStepMsg struct{}
type sendResultMsg struct {
	id  string
	err error
}

func (m *SendModel) Init() tea.Cmd {
	m.steps[0].status = stepActive
	return tea.Batch(m.spinner.Tick, m.doNextStep())
}

func (m *SendModel) doNextStep() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(500 * time.Millisecond) // Artificial delay for "verbose" feel
		return nextStepMsg{}
	}
}

func (m *SendModel) sendNotification() tea.Cmd {
	return func() tea.Msg {
		client := GetNotifyServiceClient()
		ctx, cancel := NewCommandContext(context.Background())
		defer cancel()

		resp, err := client.SendNotification(ctx, &notifyv1.SendNotificationRequest{
			ServiceId:    m.serviceID,
			Topic:        m.topic,
			Payload:      m.payload,
			Destinations: m.dest,
		})
		if err != nil {
			return sendResultMsg{err: err}
		}
		return sendResultMsg{id: resp.NotificationId}
	}
}

func (m *SendModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || (m.done && msg.String() == "q") {
			m.quitting = true
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case nextStepMsg:
		m.steps[m.current].status = stepDone
		m.current++
		if m.current < len(m.steps) {
			m.steps[m.current].status = stepActive
			if m.current == 2 { // "Sending notification" step
				return m, m.sendNotification()
			}
			return m, m.doNextStep()
		}
	case sendResultMsg:
		if msg.err != nil {
			m.steps[m.current].status = stepFailed
			m.err = msg.err
		} else {
			m.steps[m.current].status = stepDone
			m.resultID = msg.id
		}
		m.done = true
		return m, nil
	}
	return m, nil
}

func (m *SendModel) View() string {
	if m.quitting {
		return ""
	}

	var s strings.Builder
	s.WriteString(titleStyle.Render("NotifyCtl Send") + "\n\n")

	for _, step := range m.steps {
		symbol := " "
		label := step.label

		switch step.status {
		case stepPending:
			symbol = "  "
		case stepActive:
			symbol = m.spinner.View()
			label = lipgloss.NewStyle().Bold(true).Render(label)
		case stepDone:
			symbol = successStyle.Render("✓ ")
		case stepFailed:
			symbol = errorStyle.Render("✗ ")
		}

		s.WriteString(fmt.Sprintf("  %s %s\n", symbol, label))
		if step.status == stepFailed && m.err != nil {
			s.WriteString(fmt.Sprintf("    %s\n", errorStyle.Render(m.err.Error())))
		}
	}

	if m.done {
		if m.err == nil {
			s.WriteString(fmt.Sprintf("\n  %s Notification sent! ID: %s\n", successStyle.Render("DONE"), idStyle.Render(m.resultID)))
		}
		s.WriteString("\n  (Press q to exit)")
	}

	return s.String()
}
