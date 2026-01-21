package cmd

import (
	tea "github.com/charmbracelet/bubbletea"
)

type UI struct {
	Model tea.Model
}

func NewUI(model tea.Model) *UI {
	return &UI{
		Model: model,
	}
}

func (u *UI) Run() error {
	p := tea.NewProgram(u.Model)
	_, err := p.Run()
	return err
}

func (u *UI) RunWithContext(opts ...tea.ProgramOption) error {
	p := tea.NewProgram(u.Model, opts...)
	_, err := p.Run()
	return err
}
