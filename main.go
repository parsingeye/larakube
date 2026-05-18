package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type mode int

const (
	modeNamespaces mode = iota
	modePods
	modeActions
	modeConfirm
	modeOutput
	modeCommandInput
)

type commandResultMsg struct {
	output string
	err    error
}

type namespacesLoadedMsg struct {
	namespaces []string
	err        error
}

type podsLoadedMsg struct {
	namespace string
	pods      []Pod
	err       error
}

type model struct {
	client          *KubectlClient
	namespaces      []string
	namespaceCursor int
	namespaceOffset int
	selectedNS      string
	pods            []Pod
	selected        map[string]bool
	podCursor       int
	podOffset       int
	actionCursor    int
	actionOffset    int
	mode            mode
	width           int
	height          int
	status          string
	output          string
	outputOffset    int
	outputXOffset   int
	customCommand   string
	running         bool
	laravelOnly     bool
	actions         []Action
	quitting        bool
}

type Action struct {
	Name         string
	Description  string
	CommandHint  string
	FeatureFlag  string
	Build        func([]Pod, string) PodCommand
	NeedsInput   bool
	InputHint    string
	Confirm      bool
	ConfirmText  string
	FormatOutput func(string) string
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init":
			if err := runInit(); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		default:
			fmt.Fprintf(os.Stderr, "error: unknown command %q\n", os.Args[1])
			fmt.Fprintf(os.Stderr, "usage: %s [init]\n", os.Args[0])
			os.Exit(1)
		}
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	client := &KubectlClient{config: config}
	m := newModel(client)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runInit() error {
	reader := bufio.NewReader(os.Stdin)

	if _, err := os.Stat(configPath); err == nil {
		confirmed, err := promptYesNo(reader, fmt.Sprintf("%s already exists. Overwrite it?", configPath), false)
		if err != nil {
			return err
		}
		if !confirmed {
			return errors.New("init cancelled")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check config %s: %w", configPath, err)
	}

	namespace, err := promptRequiredValue(reader, "Laravel namespace to include")
	if err != nil {
		return err
	}

	cfg := DefaultConfig()
	cfg.LaravelNamespaces = []string{namespace}
	if err := WriteConfig(configPath, cfg); err != nil {
		return err
	}

	fmt.Printf("Created %s for namespace %q\n", configPath, namespace)
	return nil
}

func promptRequiredValue(reader *bufio.Reader, label string) (string, error) {
	for {
		fmt.Printf("%s: ", label)
		value, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("read %s: %w", strings.ToLower(label), err)
		}

		value = strings.TrimSpace(value)
		if value != "" {
			return value, nil
		}

		fmt.Println("A value is required.")
	}
}

func promptYesNo(reader *bufio.Reader, label string, defaultYes bool) (bool, error) {
	suffix := "[y/N]"
	if defaultYes {
		suffix = "[Y/n]"
	}

	for {
		fmt.Printf("%s %s: ", label, suffix)
		answer, err := reader.ReadString('\n')
		if err != nil {
			return false, fmt.Errorf("read confirmation: %w", err)
		}

		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "":
			return defaultYes, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}

		fmt.Println("Please answer yes or no.")
	}
}

func newModel(client *KubectlClient) model {
	actions := []Action{
		{
			Name:        "Failed Queue Jobs",
			Description: "List jobs that have failed and are waiting for inspection or retry.",
			CommandHint: "php -d memory_limit=-1 artisan queue:failed --no-ansi",
			FormatOutput: func(output string) string {
				return formatFailedQueueOutput(output)
			},
			Build: func(_ []Pod, _ string) PodCommand {
				return PodCommand{Label: "php -d memory_limit=-1 artisan queue:failed --no-ansi", Command: []string{"php", "-d", "memory_limit=-1", "artisan", "queue:failed", "--no-ansi"}}
			},
		},
		{
			Name:        "Laravel Version",
			Description: "Show the Laravel version installed in the selected pods.",
			CommandHint: "php artisan --version",
			Build: func(_ []Pod, _ string) PodCommand {
				return PodCommand{Label: "php artisan --version", Command: []string{"php", "artisan", "--version"}}
			},
		},
		{
			Name:        "App Environment",
			Description: "Display the current Laravel application environment.",
			CommandHint: "php artisan env",
			Build: func(_ []Pod, _ string) PodCommand {
				return PodCommand{Label: "php artisan env", Command: []string{"php", "artisan", "env"}}
			},
		},
		{
			Name:        "Route List",
			Description: "List registered application routes.",
			CommandHint: "php artisan route:list",
			Build: func(_ []Pod, _ string) PodCommand {
				return PodCommand{Label: "php artisan route:list", Command: []string{"php", "artisan", "route:list"}}
			},
		},
		{
			Name:        "Schedule List",
			Description: "Show scheduled tasks and their next run times.",
			CommandHint: "php artisan schedule:list",
			Build: func(_ []Pod, _ string) PodCommand {
				return PodCommand{Label: "php artisan schedule:list", Command: []string{"php", "artisan", "schedule:list"}}
			},
		},
		{
			Name:        "Horizon Status",
			Description: "Check whether Laravel Horizon is running in the selected pods.",
			CommandHint: "php artisan horizon:status",
			FeatureFlag: "horizon",
			Build: func(_ []Pod, _ string) PodCommand {
				return PodCommand{Label: "php artisan horizon:status", Command: []string{"php", "artisan", "horizon:status"}}
			},
		},
		{
			Name:        "Config Overview",
			Description: "Show environment-related Laravel runtime details.",
			CommandHint: "php artisan about --only=environment",
			Build: func(_ []Pod, _ string) PodCommand {
				return PodCommand{Label: "php artisan about --only=environment", Command: []string{"php", "artisan", "about", "--only=environment"}}
			},
		},
		{
			Name:        "Migrations Status",
			Description: "List migrations and whether they have been run.",
			CommandHint: "php artisan migrate:status --no-ansi",
			Build: func(_ []Pod, _ string) PodCommand {
				return PodCommand{Label: "php artisan migrate:status --no-ansi", Command: []string{"php", "artisan", "migrate:status", "--no-ansi"}}
			},
		},
		{
			Name:        "Queue Status",
			Description: "Check whether the default queue is backed up past the threshold.",
			CommandHint: "php artisan queue:monitor default --max=1",
			Build: func(_ []Pod, _ string) PodCommand {
				return PodCommand{Label: "php artisan queue:monitor default --max=1", Command: []string{"php", "artisan", "queue:monitor", "default", "--max=1"}}
			},
		},
		{
			Name:        "Optimize Clear",
			Description: "Clear cached bootstrap files like config, routes, events, and views.",
			CommandHint: "php artisan optimize:clear",
			Confirm:     true,
			ConfirmText: "Run php artisan optimize:clear on the selected pods?",
			Build: func(_ []Pod, _ string) PodCommand {
				return PodCommand{Label: "php artisan optimize:clear", Command: []string{"php", "artisan", "optimize:clear"}}
			},
		},
		{
			Name:        "Tail Laravel Log",
			Description: "Show recent application logs using the configured log source.",
			CommandHint: buildLogCommandHint(client.config),
			Build: func(_ []Pod, _ string) PodCommand {
				return buildLogCommand(client.config)
			},
		},
		{
			Name:        "Custom Artisan",
			Description: "Run any Artisan command you type against the selected pods.",
			CommandHint: "php artisan <your command>",
			NeedsInput:  true,
			InputHint:   "route:list",
			Build: func(_ []Pod, input string) PodCommand {
				return PodCommand{Label: "php artisan " + input, Command: []string{"sh", "-lc", "php artisan " + input}}
			},
		},
		{
			Name:        "Custom Shell",
			Description: "Run any shell command directly inside the selected pods.",
			CommandHint: "sh -lc '<your command>'",
			NeedsInput:  true,
			InputHint:   "php -v",
			Build: func(_ []Pod, input string) PodCommand {
				return PodCommand{Label: input, Command: []string{"sh", "-lc", input}}
			},
		},
	}

	filteredActions := make([]Action, 0, len(actions))
	for _, action := range actions {
		if action.FeatureFlag != "" && !client.config.FeatureEnabled(action.FeatureFlag) {
			continue
		}
		filteredActions = append(filteredActions, action)
	}

	return model{
		client:      client,
		selected:    map[string]bool{},
		mode:        modeNamespaces,
		status:      "Loading namespaces...",
		laravelOnly: true,
		actions:     filteredActions,
	}
}

func (m model) Init() tea.Cmd {
	return loadNamespacesCmd(m.client)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case namespacesLoadedMsg:
		m.running = false
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		m.namespaces = msg.namespaces
		m.namespaceCursor = 0
		m.namespaceOffset = 0
		m.status = fmt.Sprintf("Loaded %d Laravel namespaces from config", len(msg.namespaces))
		return m, nil
	case podsLoadedMsg:
		m.running = false
		if msg.err != nil {
			m.status = msg.err.Error()
			m.pods = nil
			return m, nil
		}
		m.selectedNS = msg.namespace
		m.pods = msg.pods
		m.selected = map[string]bool{}
		m.podCursor = 0
		m.podOffset = 0
		m.mode = modePods
		m.status = fmt.Sprintf("Loaded %d pods in %s (%d Laravel-like)", len(msg.pods), msg.namespace, m.countLaravelPods())
		return m, nil
	case commandResultMsg:
		m.running = false
		output := msg.output
		if formatter := m.actions[m.actionCursor].FormatOutput; formatter != nil {
			output = formatter(output)
		}
		if msg.err != nil {
			m.output = output
			if m.output == "" {
				m.output = msg.err.Error()
			}
			m.status = fmt.Sprintf("Command failed: %v", msg.err)
		} else {
			m.output = output
			m.status = "Command finished"
		}
		m.mode = modeOutput
		m.outputOffset = 0
		m.outputXOffset = 0
		return m, nil
	case tea.KeyMsg:
		if m.running {
			switch msg.String() {
			case "ctrl+c", "q":
				m.quitting = true
				return m, tea.Quit
			}
			return m, nil
		}

		switch m.mode {
		case modeNamespaces:
			return m.updateNamespaces(msg)
		case modePods:
			return m.updatePods(msg)
		case modeActions:
			return m.updateActions(msg)
		case modeConfirm:
			return m.updateConfirm(msg)
		case modeOutput:
			return m.updateOutput(msg)
		case modeCommandInput:
			return m.updateCommandInput(msg)
		}
	}

	return m, nil
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	var body strings.Builder
	body.WriteString(titleStyle.Render("Larakube"))
	body.WriteString("\n")
	body.WriteString(subtleStyle.Render("Laravel-aware Kubernetes TUI"))
	body.WriteString("\n\n")

	switch m.mode {
	case modeNamespaces:
		body.WriteString(m.renderNamespacesView())
	case modePods:
		body.WriteString(m.renderPodsView())
	case modeActions:
		body.WriteString(m.renderActionsView())
	case modeConfirm:
		body.WriteString(m.renderConfirmView())
	case modeOutput:
		body.WriteString(m.renderOutputView())
	case modeCommandInput:
		body.WriteString(m.renderCommandInputView())
	}

	body.WriteString("\n\n")
	body.WriteString(helpStyle.Render(m.helpText()))
	body.WriteString("\n")
	body.WriteString(statusStyle.Render(m.status))

	return appStyle.Width(m.width).Render(body.String())
}

func (m model) updateNamespaces(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "r":
		m.running = true
		m.status = "Refreshing namespaces..."
		return m, loadNamespacesCmd(m.client)
	case "up", "k":
		if m.namespaceCursor > 0 {
			m.namespaceCursor--
			m.namespaceOffset = clampCursorOffset(m.namespaceCursor, m.namespaceOffset, m.listPageSize())
		}
	case "down", "j":
		if m.namespaceCursor < len(m.namespaces)-1 {
			m.namespaceCursor++
			m.namespaceOffset = clampCursorOffset(m.namespaceCursor, m.namespaceOffset, m.listPageSize())
		}
	case "pgdown", "ctrl+f":
		step := m.listPageSize()
		if step < 1 {
			step = 1
		}
		m.namespaceCursor = minInt(len(m.namespaces)-1, m.namespaceCursor+step)
		m.namespaceOffset = clampCursorOffset(m.namespaceCursor, m.namespaceOffset, step)
	case "pgup", "ctrl+b":
		step := m.listPageSize()
		if step < 1 {
			step = 1
		}
		m.namespaceCursor = maxInt(0, m.namespaceCursor-step)
		m.namespaceOffset = clampCursorOffset(m.namespaceCursor, m.namespaceOffset, step)
	case "enter":
		if len(m.namespaces) == 0 {
			m.status = "No namespaces available"
			return m, nil
		}
		namespace := m.namespaces[m.namespaceCursor]
		m.running = true
		m.status = fmt.Sprintf("Loading pods in %s...", namespace)
		return m, loadPodsCmd(m.client, namespace)
	}
	return m, nil
}

func (m model) updatePods(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "esc", "backspace":
		m.mode = modeNamespaces
		m.status = "Back to namespaces"
		return m, nil
	case "r":
		if m.selectedNS == "" {
			m.mode = modeNamespaces
			m.status = "Select a namespace"
			return m, nil
		}
		m.running = true
		m.status = fmt.Sprintf("Refreshing pods in %s...", m.selectedNS)
		return m, loadPodsCmd(m.client, m.selectedNS)
	case "l":
		m.laravelOnly = !m.laravelOnly
		visible := m.visiblePods()
		if m.podCursor >= len(visible) && len(visible) > 0 {
			m.podCursor = len(visible) - 1
		}
		if len(visible) == 0 {
			m.podCursor = 0
		}
		m.podOffset = clampCursorOffset(m.podCursor, 0, m.podsPageSize())
		if m.laravelOnly {
			m.status = fmt.Sprintf("Showing Laravel-like pods in %s (%d visible)", m.selectedNS, len(visible))
		} else {
			m.status = fmt.Sprintf("Showing all pods in %s (%d visible)", m.selectedNS, len(visible))
		}
	case "up", "k":
		if m.podCursor > 0 {
			m.podCursor--
			m.podOffset = clampCursorOffset(m.podCursor, m.podOffset, m.podsPageSize())
		}
	case "down", "j":
		if m.podCursor < len(m.visiblePods())-1 {
			m.podCursor++
			m.podOffset = clampCursorOffset(m.podCursor, m.podOffset, m.podsPageSize())
		}
	case "pgdown", "ctrl+f":
		step := m.podsPageSize()
		if step < 1 {
			step = 1
		}
		m.podCursor = minInt(len(m.visiblePods())-1, m.podCursor+step)
		m.podOffset = clampCursorOffset(m.podCursor, m.podOffset, step)
	case "pgup", "ctrl+b":
		step := m.podsPageSize()
		if step < 1 {
			step = 1
		}
		m.podCursor = maxInt(0, m.podCursor-step)
		m.podOffset = clampCursorOffset(m.podCursor, m.podOffset, step)
	case " ":
		visible := m.visiblePods()
		if len(visible) > 0 {
			pod := visible[m.podCursor]
			m.selected[pod.Key()] = !m.selected[pod.Key()]
		}
	case "enter":
		if len(m.currentSelection()) == 0 {
			m.status = "Select at least one pod"
			return m, nil
		}
		m.mode = modeActions
		m.status = fmt.Sprintf("%d pod(s) selected in %s", len(m.currentSelection()), m.selectedNS)
	}
	return m, nil
}

func (m model) updateActions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "esc", "backspace":
		m.mode = modePods
		m.status = "Back to pod selection"
		return m, nil
	case "up", "k":
		if m.actionCursor > 0 {
			m.actionCursor--
			m.actionOffset = clampCursorOffset(m.actionCursor, m.actionOffset, m.actionsPageSize())
		}
	case "down", "j":
		if m.actionCursor < len(m.actions)-1 {
			m.actionCursor++
			m.actionOffset = clampCursorOffset(m.actionCursor, m.actionOffset, m.actionsPageSize())
		}
	case "pgdown", "ctrl+f":
		step := m.actionsPageSize()
		if step < 1 {
			step = 1
		}
		m.actionCursor = minInt(len(m.actions)-1, m.actionCursor+step)
		m.actionOffset = clampCursorOffset(m.actionCursor, m.actionOffset, step)
	case "pgup", "ctrl+b":
		step := m.actionsPageSize()
		if step < 1 {
			step = 1
		}
		m.actionCursor = maxInt(0, m.actionCursor-step)
		m.actionOffset = clampCursorOffset(m.actionCursor, m.actionOffset, step)
	case "enter":
		action := m.actions[m.actionCursor]
		if action.Confirm {
			m.mode = modeConfirm
			m.status = action.ConfirmText
			return m, nil
		}
		if action.NeedsInput {
			m.mode = modeCommandInput
			m.customCommand = ""
			m.status = "Enter command and press Enter"
			return m, nil
		}
		return m.runSelectedAction(action, "")
	}
	return m, nil
}

func (m model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "esc", "backspace", "n":
		m.mode = modeActions
		m.status = "Cancelled"
		return m, nil
	case "enter", "y":
		return m.runSelectedAction(m.actions[m.actionCursor], "")
	}
	return m, nil
}

func (m model) updateOutput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "esc", "backspace":
		m.mode = modeActions
		m.status = "Back to actions"
	case "up", "k":
		m.outputOffset = maxInt(0, m.outputOffset-1)
	case "down", "j":
		m.outputOffset = minInt(m.maxOutputOffset(), m.outputOffset+1)
	case "left", "h":
		m.outputXOffset = maxInt(0, m.outputXOffset-4)
	case "right", "l":
		m.outputXOffset = minInt(m.maxOutputXOffset(), m.outputXOffset+4)
	case "pgup", "ctrl+b":
		m.outputOffset = maxInt(0, m.outputOffset-m.outputPageSize())
	case "pgdown", "ctrl+f":
		m.outputOffset = minInt(m.maxOutputOffset(), m.outputOffset+m.outputPageSize())
	case "r":
		action := m.actions[m.actionCursor]
		if action.NeedsInput && strings.TrimSpace(m.customCommand) == "" {
			m.mode = modeCommandInput
			m.status = "Enter command and press Enter"
			return m, nil
		}
		return m.runSelectedAction(action, m.customCommand)
	}
	return m, nil
}

func (m model) updateCommandInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.mode = modeActions
		m.status = "Back to actions"
		return m, nil
	case "enter":
		m.customCommand = strings.TrimSpace(m.customCommand)
		if m.customCommand == "" {
			m.status = "Command cannot be empty"
			return m, nil
		}
		action := m.actions[m.actionCursor]
		return m.runSelectedAction(action, m.customCommand)
	case "backspace":
		if len(m.customCommand) > 0 {
			m.customCommand = m.customCommand[:len(m.customCommand)-1]
		}
	default:
		if msg.Type == tea.KeyRunes {
			m.customCommand += string(msg.Runes)
		}
	}
	return m, nil
}

func (m model) renderNamespacesView() string {
	if len(m.namespaces) == 0 {
		return panelStyle.Render("No namespaces found. Press r to refresh.")
	}

	var lines []string
	start, end := visibleRange(len(m.namespaces), m.namespaceOffset, m.listPageSize())
	for i := start; i < end; i++ {
		namespace := m.namespaces[i]
		cursor := " "
		if i == m.namespaceCursor {
			cursor = ">"
		}
		lines = append(lines, fmt.Sprintf("%s %s", cursor, namespace))
	}
	lines = append(lines, "")
	lines = append(lines, subtleStyle.Render(m.scrollSummary(start, end, len(m.namespaces))))

	return panelStyle.Render(strings.Join(lines, "\n"))
}

func (m model) renderPodsView() string {
	visible := m.visiblePods()
	if len(visible) == 0 {
		return panelStyle.Render(fmt.Sprintf("No pods found in %s. Press r to refresh or esc for namespaces.", m.selectedNS))
	}

	var lines []string
	lines = append(lines, labelStyle.Render("Namespace: ")+m.selectedNS)
	lines = append(lines, "")
	start, end := visibleRange(len(visible), m.podOffset, m.podsPageSize())
	for i := start; i < end; i++ {
		pod := visible[i]
		cursor := " "
		if i == m.podCursor {
			cursor = ">"
		}
		checked := " "
		if m.selected[pod.Key()] {
			checked = "x"
		}
		label := pod.Name
		if pod.LaravelLike {
			label += subtleStyle.Render("  [laravel]")
		}
		meta := fmt.Sprintf("[%s] %s", pod.Status, strings.Join(pod.Containers, ", "))
		lines = append(lines, fmt.Sprintf("%s [%s] %s", cursor, checked, label))
		lines = append(lines, subtleStyle.Render("    "+meta))
	}
	lines = append(lines, "")
	lines = append(lines, subtleStyle.Render(m.scrollSummary(start, end, len(visible))))

	return panelStyle.Render(strings.Join(lines, "\n"))
}

func (m model) renderActionsView() string {
	selection := m.currentSelection()
	var lines []string
	lines = append(lines, labelStyle.Render("Namespace: ")+m.selectedNS)
	lines = append(lines, labelStyle.Render("Selected Pods:"))
	for _, pod := range selection {
		lines = append(lines, fmt.Sprintf("- %s", pod.Name))
	}
	lines = append(lines, "")
	lines = append(lines, labelStyle.Render("Actions:"))
	start, end := visibleRange(len(m.actions), m.actionOffset, m.actionsPageSize())
	for i := start; i < end; i++ {
		action := m.actions[i]
		cursor := " "
		if i == m.actionCursor {
			cursor = ">"
		}
		lines = append(lines, fmt.Sprintf("%s %s", cursor, action.Name))
		lines = append(lines, subtleStyle.Render("    "+action.Description))
		if action.CommandHint != "" {
			lines = append(lines, subtleStyle.Render("    Command: "+action.CommandHint))
		}
	}
	lines = append(lines, "")
	lines = append(lines, subtleStyle.Render(m.scrollSummary(start, end, len(m.actions))))
	return panelStyle.Render(strings.Join(lines, "\n"))
}

func (m model) renderOutputView() string {
	if strings.TrimSpace(m.output) == "" {
		return panelStyle.Render("No output captured.")
	}

	lines := clipOutputLines(m.output, m.outputXOffset, m.outputContentWidth())
	pageSize := m.outputPageSize()
	start, end := visibleRange(len(lines), m.outputOffset, pageSize)
	visible := lines[start:end]
	visible = append(visible, "")
	visible = append(visible, subtleStyle.Render(fmt.Sprintf("%s  cols %d-%d", m.scrollSummary(start, end, lineCount(m.output)), m.outputXOffset+1, minInt(maxLineWidth(m.output), m.outputXOffset+m.outputContentWidth()))))
	return panelStyle.Render(strings.Join(visible, "\n"))
}

func (m model) renderConfirmView() string {
	action := m.actions[m.actionCursor]
	lines := []string{
		labelStyle.Render("Confirm Action"),
		"",
		action.ConfirmText,
		fmt.Sprintf("Namespace: %s", m.selectedNS),
		fmt.Sprintf("Selected pods: %d", len(m.currentSelection())),
		"",
		"Press y or Enter to confirm.",
		"Press n or Esc to cancel.",
	}
	return panelStyle.Render(strings.Join(lines, "\n"))
}

func (m model) renderCommandInputView() string {
	action := m.actions[m.actionCursor]
	prompt := fmt.Sprintf("Namespace: %s\n%s\nHint: %s\n\n> %s", m.selectedNS, action.Description, action.InputHint, m.customCommand)
	return panelStyle.Render(prompt)
}

func (m model) helpText() string {
	switch m.mode {
	case modeNamespaces:
		return "j/k move  pgup/pgdn page  enter select namespace  r refresh  q quit"
	case modePods:
		return "j/k move  pgup/pgdn page  space select  enter actions  l toggle filter  r refresh  esc namespaces  q quit"
	case modeActions:
		return "j/k move  pgup/pgdn page  enter run  esc back  q quit"
	case modeConfirm:
		return "y/enter confirm  n/esc cancel  q quit"
	case modeOutput:
		return "j/k scroll  h/l horizontal  pgup/pgdn page  r rerun  esc back  q quit"
	case modeCommandInput:
		return "type command  enter run  esc back  q quit"
	default:
		return "q quit"
	}
}

func (m model) currentSelection() []Pod {
	var pods []Pod
	for _, pod := range m.pods {
		if m.selected[pod.Key()] {
			pods = append(pods, pod)
		}
	}
	return pods
}

func (m model) listPageSize() int {
	return maxInt(1, m.availableContentHeight()-2)
}

func (m model) podsPageSize() int {
	return maxInt(1, (m.availableContentHeight()-4)/2)
}

func (m model) actionsPageSize() int {
	reserved := 4 + len(m.currentSelection())
	return maxInt(1, (m.availableContentHeight()-reserved)/2)
}

func (m model) outputPageSize() int {
	return maxInt(1, m.availableContentHeight()-2)
}

func (m model) maxOutputOffset() int {
	lines := strings.Split(m.output, "\n")
	return maxInt(0, len(lines)-m.outputPageSize())
}

func (m model) maxOutputXOffset() int {
	return maxInt(0, maxLineWidth(m.output)-m.outputContentWidth())
}

func (m model) availableContentHeight() int {
	if m.height <= 0 {
		return 12
	}

	reserved := 8
	return maxInt(3, m.height-reserved)
}

func (m model) outputContentWidth() int {
	if m.width <= 0 {
		return 80
	}

	reserved := 10
	return maxInt(20, m.width-reserved)
}

func (m model) scrollSummary(start, end, total int) string {
	if total == 0 {
		return "0 items"
	}
	return fmt.Sprintf("showing %d-%d of %d", start+1, end, total)
}

func visibleRange(total, offset, pageSize int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	if pageSize < 1 {
		pageSize = 1
	}
	maxOffset := maxInt(0, total-pageSize)
	offset = maxInt(0, minInt(offset, maxOffset))
	end := minInt(total, offset+pageSize)
	return offset, end
}

func clampCursorOffset(cursor, offset, pageSize int) int {
	if pageSize < 1 {
		pageSize = 1
	}
	if cursor < offset {
		return cursor
	}
	if cursor >= offset+pageSize {
		return cursor - pageSize + 1
	}
	return offset
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clipOutputLines(output string, xOffset, width int) []string {
	if width < 1 {
		width = 1
	}

	rawLines := strings.Split(output, "\n")
	clipped := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if xOffset >= len(line) {
			clipped = append(clipped, "")
			continue
		}
		end := minInt(len(line), xOffset+width)
		clipped = append(clipped, line[xOffset:end])
	}
	return clipped
}

func maxLineWidth(output string) int {
	width := 0
	for _, line := range strings.Split(output, "\n") {
		if len(line) > width {
			width = len(line)
		}
	}
	return width
}

func lineCount(output string) int {
	if output == "" {
		return 0
	}
	return len(strings.Split(output, "\n"))
}

func formatFailedQueueOutput(output string) string {
	formatted, ok := formatAsciiTableAsRecords(output)
	if ok {
		return formatted
	}
	return output
}

func formatAsciiTableAsRecords(output string) (string, bool) {
	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))
	changed := false
	for i := 0; i < len(lines); {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "+") || i+2 >= len(lines) {
			result = append(result, lines[i])
			i++
			continue
		}

		headerLine := strings.TrimSpace(lines[i+1])
		separatorLine := strings.TrimSpace(lines[i+2])
		if !isTableRow(headerLine) || !strings.HasPrefix(separatorLine, "+") {
			result = append(result, lines[i])
			i++
			continue
		}

		headers := parseTableRow(headerLine)
		if len(headers) == 0 {
			result = append(result, lines[i])
			i++
			continue
		}

		rowCount := 0
		j := i + 3
		for j < len(lines) {
			row := strings.TrimSpace(lines[j])
			if strings.HasPrefix(row, "+") {
				j++
				break
			}
			if !isTableRow(row) {
				break
			}
			cells := parseTableRow(row)
			if len(cells) != len(headers) {
				break
			}
			rowCount++
			result = append(result, fmt.Sprintf("Row %d", rowCount))
			for idx, header := range headers {
				result = append(result, fmt.Sprintf("%s: %s", header, cells[idx]))
			}
			result = append(result, "")
			j++
		}

		if rowCount > 0 {
			changed = true
			i = j
			continue
		}

		result = append(result, lines[i])
		i++
	}

	return strings.TrimSpace(strings.Join(result, "\n")), changed
}

func isTableRow(line string) bool {
	return strings.HasPrefix(line, "|") && strings.HasSuffix(line, "|")
}

func parseTableRow(line string) []string {
	parts := strings.Split(strings.Trim(line, "|"), "|")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		values = append(values, strings.TrimSpace(part))
	}
	return values
}

func buildLogCommand(config Config) PodCommand {
	switch config.Logs.Source {
	case "file":
		label := fmt.Sprintf("tail -n %d %s", config.Logs.Tail, config.Logs.Path)
		return PodCommand{
			Label:   label,
			Command: []string{"sh", "-lc", label},
		}
	default:
		args := []string{"--tail", fmt.Sprintf("%d", config.Logs.Tail)}
		if config.Logs.Container != "" {
			args = append(args, "-c", config.Logs.Container)
		}
		return PodCommand{
			Label:   "kubectl logs",
			Command: args,
			Logs:    true,
		}
	}
}

func buildLogCommandHint(config Config) string {
	switch config.Logs.Source {
	case "file":
		return fmt.Sprintf("tail -n %d %s", config.Logs.Tail, config.Logs.Path)
	default:
		hint := fmt.Sprintf("kubectl logs --tail=%d", config.Logs.Tail)
		if config.Logs.Container != "" {
			hint += " -c " + config.Logs.Container
		}
		return hint
	}
}

func (m model) visiblePods() []Pod {
	if !m.laravelOnly {
		return m.pods
	}

	visible := make([]Pod, 0, len(m.pods))
	for _, pod := range m.pods {
		if pod.LaravelLike {
			visible = append(visible, pod)
		}
	}
	return visible
}

func (m model) countLaravelPods() int {
	count := 0
	for _, pod := range m.pods {
		if pod.LaravelLike {
			count++
		}
	}
	return count
}

func (m model) runSelectedAction(action Action, input string) (tea.Model, tea.Cmd) {
	selection := m.currentSelection()
	if len(selection) == 0 {
		m.status = "Select at least one pod"
		return m, nil
	}

	if action.NeedsInput {
		input = strings.TrimSpace(input)
		if input == "" {
			m.status = "Command cannot be empty"
			m.mode = modeCommandInput
			return m, nil
		}
		m.customCommand = input
	}

	cmd := action.Build(selection, input)
	m.running = true
	m.status = fmt.Sprintf("Running %q on %d pod(s)...", cmd.Label, len(selection))
	m.output = ""
	return m, runPodCommandCmd(m.client, selection, cmd)
}

func loadNamespacesCmd(client *KubectlClient) tea.Cmd {
	return func() tea.Msg {
		namespaces, err := client.ListNamespaces()
		return namespacesLoadedMsg{namespaces: namespaces, err: err}
	}
}

func loadPodsCmd(client *KubectlClient, namespace string) tea.Cmd {
	return func() tea.Msg {
		pods, err := client.ListPods(namespace)
		return podsLoadedMsg{namespace: namespace, pods: pods, err: err}
	}
}

func runPodCommandCmd(client *KubectlClient, pods []Pod, cmd PodCommand) tea.Cmd {
	return func() tea.Msg {
		out, err := client.RunCommandAcrossPods(pods, cmd)
		return commandResultMsg{output: out, err: err}
	}
}

func sortPods(pods []Pod) {
	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Name < pods[j].Name
	})
}

func sortStrings(values []string) {
	sort.Strings(values)
}

var errNoPods = errors.New("no pods found via kubectl")
var errNoNamespaces = errors.New("no namespaces found via kubectl")
