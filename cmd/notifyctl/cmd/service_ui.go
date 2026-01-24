package cmd

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

var (
	keywordStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("204")).Bold(true)
	idStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("197")).Bold(true)
)

// ListServicesModel
type ListServicesModel struct {
	services []*notifyv1.ServiceInfo
	err      error
	quitting bool
}

func (m ListServicesModel) Init() tea.Cmd { return nil }

func (m ListServicesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m ListServicesModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("\n  Error: %v\n", m.err))
	}

	var s strings.Builder
	s.WriteString(titleStyle.Render("Registered Services") + "\n\n")

	if len(m.services) == 0 {
		s.WriteString("  No services found.\n")
	} else {
		// Simple table view
		header := headerStyle.Render(fmt.Sprintf("%-36s  %-20s  %s", "ID", "NAME", "WEBHOOK URL"))
		s.WriteString(header + "\n")

		for _, svc := range m.services {
			s.WriteString(fmt.Sprintf("%-36s  %-20s  %s\n",
				idStyle.Render(svc.Id),
				keywordStyle.Render(svc.Name),
				svc.WebhookUrl,
			))
		}
	}

	s.WriteString("\n  (Press q to quit)")
	return s.String()
}

// ServiceStatusModel
type ServiceStatusModel struct {
	Title    string
	Message  string
	ID       string
	Key      string
	Err      error
	quitting bool
}

func (m ServiceStatusModel) Init() tea.Cmd { return nil }

func (m ServiceStatusModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyMsg:
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m ServiceStatusModel) View() string {
	var s strings.Builder
	if m.Err != nil {
		s.WriteString(errorStyle.Render("FAILED") + " " + m.Title + "\n")
		s.WriteString(fmt.Sprintf("  %v\n", m.Err))
	} else {
		s.WriteString(successStyle.Render("SUCCESS") + " " + m.Title + "\n")
		s.WriteString(fmt.Sprintf("  %s\n", m.Message))
		if m.ID != "" {
			s.WriteString(fmt.Sprintf("  ID:      %s\n", idStyle.Render(m.ID)))
		}
		if m.Key != "" {
			s.WriteString(fmt.Sprintf("  API Key: %s\n", keywordStyle.Render(m.Key)))
		}
	}
	s.WriteString("\n  (Press any key to exit)")
	return s.String()
}
