package cmd

import (
	tea "github.com/charmbracelet/bubbletea"
)

type UI struct {
	model tea.Model
}

func NewUI(model tea.Model) *UI {
	return &UI{
		model: model,
	}
}

func (u *UI) Run(opts ...tea.ProgramOption) error {
	p := tea.NewProgram(u.model, opts...)
	_, err := p.Run()
	return err
}
