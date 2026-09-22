package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mdfranz/mcpctl/internal/config"
)

// formModel is the add/edit server form. Renaming isn't supported in
// the MVP (delete+add instead, per the plan), so editing fixes the name
// and starts focus on the next field.
type formModel struct {
	editing      bool
	originalName string
	serverType   config.ServerType

	nameInput    textinput.Model
	commandInput textinput.Model
	argsInput    textinput.Model
	envArea      textarea.Model

	urlInput    textinput.Model
	headersArea textarea.Model
	bearerInput textinput.Model

	focus  int
	errMsg string
}

func newAddForm() formModel {
	f := formModel{serverType: config.ServerTypeStdio}

	f.nameInput = textinput.New()
	f.nameInput.Placeholder = "server-name"
	f.nameInput.Focus()

	f.commandInput = textinput.New()
	f.commandInput.Placeholder = "command"

	f.argsInput = textinput.New()
	f.argsInput.Placeholder = "arg1 arg2 ..."

	f.envArea = textarea.New()
	f.envArea.Placeholder = "NAME=value\nNAME=$\nNAME=$:default"
	f.envArea.SetHeight(5)
	f.envArea.ShowLineNumbers = false

	f.urlInput = textinput.New()
	f.urlInput.Placeholder = "https://example.com/mcp"

	f.headersArea = textarea.New()
	f.headersArea.Placeholder = "X-Header=value\nAuthorization=$TOKEN_ENV_VAR"
	f.headersArea.SetHeight(4)
	f.headersArea.ShowLineNumbers = false

	f.bearerInput = textinput.New()
	f.bearerInput.Placeholder = "BEARER_TOKEN_ENV_VAR (optional)"

	return f
}

func newEditForm(name string, srv config.Server) formModel {
	f := newAddForm()
	f.editing = true
	f.originalName = name
	f.serverType = srv.Type
	f.nameInput.SetValue(name)
	f.nameInput.Blur()

	switch srv.Type {
	case config.ServerTypeStdio:
		f.commandInput.SetValue(srv.Command)
		f.argsInput.SetValue(strings.Join(srv.Args, " "))
		f.envArea.SetValue(strings.Join(renderEnvLines(srv.Env), "\n"))
		f.commandInput.Focus()
	case config.ServerTypeRemote:
		f.urlInput.SetValue(srv.URL)
		f.headersArea.SetValue(strings.Join(renderHeaderLines(srv.Headers), "\n"))
		f.bearerInput.SetValue(srv.BearerTokenEnvVar)
		f.urlInput.Focus()
	}
	f.focus = 1
	return f
}

func (f *formModel) fieldNames() []string {
	if f.serverType == config.ServerTypeStdio {
		return []string{"name", "command", "args", "env"}
	}
	return []string{"name", "url", "headers", "bearer"}
}

func (f *formModel) blurAll() {
	f.nameInput.Blur()
	f.commandInput.Blur()
	f.argsInput.Blur()
	f.envArea.Blur()
	f.urlInput.Blur()
	f.headersArea.Blur()
	f.bearerInput.Blur()
}

func (f *formModel) focusField(idx int) {
	names := f.fieldNames()
	lo := 0
	if f.editing {
		lo = 1
	}
	hi := len(names) - 1
	if idx < lo {
		idx = lo
	}
	if idx > hi {
		idx = hi
	}
	f.blurAll()
	f.focus = idx
	switch names[idx] {
	case "name":
		f.nameInput.Focus()
	case "command":
		f.commandInput.Focus()
	case "args":
		f.argsInput.Focus()
	case "env":
		f.envArea.Focus()
	case "url":
		f.urlInput.Focus()
	case "headers":
		f.headersArea.Focus()
	case "bearer":
		f.bearerInput.Focus()
	}
}

func (f *formModel) next() { f.focusField(f.focus + 1) }
func (f *formModel) prev() { f.focusField(f.focus - 1) }

func (f *formModel) toggleType() {
	if f.editing {
		return
	}
	if f.serverType == config.ServerTypeStdio {
		f.serverType = config.ServerTypeRemote
	} else {
		f.serverType = config.ServerTypeStdio
	}
	f.focusField(0)
}

func (f formModel) updateFocused(msg tea.Msg) (formModel, tea.Cmd) {
	var cmd tea.Cmd
	switch f.fieldNames()[f.focus] {
	case "name":
		f.nameInput, cmd = f.nameInput.Update(msg)
	case "command":
		f.commandInput, cmd = f.commandInput.Update(msg)
	case "args":
		f.argsInput, cmd = f.argsInput.Update(msg)
	case "env":
		f.envArea, cmd = f.envArea.Update(msg)
	case "url":
		f.urlInput, cmd = f.urlInput.Update(msg)
	case "headers":
		f.headersArea, cmd = f.headersArea.Update(msg)
	case "bearer":
		f.bearerInput, cmd = f.bearerInput.Update(msg)
	}
	return f, cmd
}

// buildServer validates the form and produces the canonical Server it
// describes.
func (f formModel) buildServer() (config.Server, error) {
	name := f.originalName
	if !f.editing {
		name = strings.TrimSpace(f.nameInput.Value())
		if err := config.ValidateNewName(name); err != nil {
			return config.Server{}, err
		}
	}

	srv := config.Server{Name: name, Type: f.serverType}
	switch f.serverType {
	case config.ServerTypeStdio:
		cmd := strings.TrimSpace(f.commandInput.Value())
		if cmd == "" {
			return config.Server{}, fmt.Errorf("command is required")
		}
		srv.Command = cmd
		if args := strings.TrimSpace(f.argsInput.Value()); args != "" {
			srv.Args = strings.Fields(args)
		}
		env, err := parseEnvLines(strings.Split(f.envArea.Value(), "\n"))
		if err != nil {
			return config.Server{}, fmt.Errorf("env: %w", err)
		}
		srv.Env = env

	case config.ServerTypeRemote:
		url := strings.TrimSpace(f.urlInput.Value())
		if url == "" {
			return config.Server{}, fmt.Errorf("url is required")
		}
		srv.URL = url
		headers, err := parseHeaderLines(strings.Split(f.headersArea.Value(), "\n"))
		if err != nil {
			return config.Server{}, fmt.Errorf("headers: %w", err)
		}
		srv.Headers = headers
		srv.BearerTokenEnvVar = strings.TrimSpace(f.bearerInput.Value())
	}
	return srv, nil
}

func (f formModel) View() string {
	var b strings.Builder

	title := "Add server"
	if f.editing {
		title = "Edit " + f.originalName
	}
	b.WriteString(headerStyle.Render(title) + "\n\n")

	if !f.editing {
		b.WriteString(fieldLabel("Name", f.focus == 0) + "\n  " + f.nameInput.View() + "\n\n")
	} else {
		b.WriteString(labelStyle.Render("Name: ") + f.originalName +
			dimStyle.Render(" (rename isn't supported in place; delete and add instead)") + "\n\n")
	}

	switch f.serverType {
	case config.ServerTypeStdio:
		b.WriteString(fieldLabel("Command", f.focus == 1) + "\n  " + f.commandInput.View() + "\n\n")
		b.WriteString(fieldLabel("Args (space-separated)", f.focus == 2) + "\n  " + f.argsInput.View() + "\n\n")
		b.WriteString(fieldLabel("Env  (NAME=value | NAME=$ | NAME=$:default)", f.focus == 3) + "\n" + f.envArea.View() + "\n\n")
	case config.ServerTypeRemote:
		b.WriteString(fieldLabel("URL", f.focus == 1) + "\n  " + f.urlInput.View() + "\n\n")
		b.WriteString(fieldLabel("Headers  (NAME=value | NAME=$ENV_VAR)", f.focus == 2) + "\n" + f.headersArea.View() + "\n\n")
		b.WriteString(fieldLabel("Bearer token env var (optional)", f.focus == 3) + "\n  " + f.bearerInput.View() + "\n\n")
	}

	if f.errMsg != "" {
		b.WriteString(errorStyle.Render("error: "+f.errMsg) + "\n\n")
	}

	typeHint := ""
	if !f.editing {
		typeHint = "ctrl+t toggle stdio/remote · "
	}
	b.WriteString(helpStyle.Render(typeHint + "tab/shift+tab move · ctrl+s save · esc cancel"))
	return b.String()
}

func fieldLabel(label string, focused bool) string {
	if focused {
		return selectedStyle.Render("▸ " + label)
	}
	return labelStyle.Render("  " + label)
}

func (m Model) updateForm(msg tea.Msg) (Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc":
			m.view = viewList
			return m, nil
		case "tab", "down":
			m.form.next()
			return m, nil
		case "shift+tab", "up":
			m.form.prev()
			return m, nil
		case "ctrl+t":
			m.form.toggleType()
			return m, nil
		case "ctrl+s":
			return m.submitForm()
		}
	}
	var cmd tea.Cmd
	m.form, cmd = m.form.updateFocused(msg)
	return m, cmd
}

// submitForm validates the form, builds a Plan for the resulting Server,
// and moves to the preview so the user sees exactly what will change
// before anything is written.
func (m Model) submitForm() (Model, tea.Cmd) {
	srv, err := m.form.buildServer()
	if err != nil {
		m.form.errMsg = err.Error()
		return m, nil
	}

	plan, err := config.BuildPlan(m.dir, map[string]config.Server{srv.Name: srv}, nil)
	if err != nil {
		m.form.errMsg = err.Error()
		return m, nil
	}

	m.plan = plan
	m.previewAction = "save " + srv.Name
	m.applyErr = nil
	m.view = viewPreview
	return m, nil
}
