package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
)

type EventMsg struct {
	Event state.Event
}

type DoneMsg struct {
	Status string
}

type row struct {
	id       string
	event    state.Event
	summary  string
	detail   string
	kind     string
	status   string
	expanded bool
	message  bool
}

type DetailLoader func(*state.Event) string

type Model struct {
	title        string
	rows         []row
	rowIndex     map[string]int
	cursor       int
	width        int
	height       int
	filter       string
	doneStatus   string
	follow       bool
	detailLoader DetailLoader
}

func NewModel(title string, loader DetailLoader) Model {
	return Model{title: title, rowIndex: map[string]int{}, follow: true, detailLoader: loader}
}

func (m Model) Init() tea.Cmd { return tea.WindowSize() }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch value := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = value.Width
		m.height = value.Height
	case tea.KeyMsg:
		switch value.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			m.follow = false
		case "down", "j":
			visible := m.visibleRows()
			if m.cursor+1 < len(visible) {
				m.cursor++
			}
			m.follow = len(visible) == 0 || m.cursor == len(visible)-1
		case "end", "G":
			m.follow = true
			m.moveToLatest()
		case "home", "g":
			m.follow = false
			m.cursor = 0
		case "enter", " ":
			visible := m.visibleRows()
			if len(visible) > 0 && m.cursor < len(visible) {
				idx := visible[m.cursor]
				if m.rows[idx].message {
					break
				}
				m.rows[idx].expanded = !m.rows[idx].expanded
				if m.rows[idx].expanded && m.rows[idx].detail == "" && m.detailLoader != nil {
					m.rows[idx].detail = m.detailLoader(&m.rows[idx].event)
				}
			}
		case "f":
			m.filter = toggleFilter(m.filter, "file_change")
			m.follow = true
			m.moveToLatest()
		case "o":
			m.filter = toggleFilter(m.filter, "command")
			m.follow = true
			m.moveToLatest()
		case "p":
			m.filter = toggleFilter(m.filter, "prompt")
			m.follow = true
			m.moveToLatest()
		}
	case EventMsg:
		m.addEvent(value.Event)
	case DoneMsg:
		m.doneStatus = value.Status
	}
	return m, nil
}

func (m Model) View() string {
	header := lipgloss.NewStyle().Bold(true).Render(m.title)
	if m.filter != "" {
		header += lipgloss.NewStyle().Faint(true).Render("  filter: " + strings.ReplaceAll(m.filter, "_", " "))
	}
	lines := []string{header, ""}
	visible := m.visibleRows()
	bodyHeight := m.bodyHeight()
	body := make([]string, 0, bodyHeight)
	start := m.viewportStart(visible, bodyHeight)
	for pos := start; pos < len(visible); pos++ {
		r := m.rows[visible[pos]]
		block := m.rowBlock(r, pos == m.cursor, bodyHeight)
		if len(body)+len(block) > bodyHeight {
			break
		}
		body = append(body, block...)
	}
	for len(body) < bodyHeight {
		body = append(body, "")
	}
	lines = append(lines, body...)
	footer := "up/down select | end follow | enter expand details | f files | o commands | p prompts | q hide"
	if m.doneStatus != "" {
		footer = "Run " + m.doneStatus + " | enter expand | q exit"
	}
	lines = append(lines, "", lipgloss.NewStyle().Faint(true).Render(footer))
	return strings.Join(lines, "\n")
}

func (m *Model) addEvent(event state.Event) {
	payload := streaming.Payload(&event)
	kind, _ := payload["kind"].(string)
	id, _ := payload["activity_id"].(string)
	if id == "" {
		id = event.ID
	}
	status, _ := payload["status"].(string)
	detail := streaming.EventDetail(&event)
	if rawPath, _ := payload["raw_log_path"].(string); rawPath != "" && kind == "prompt" {
		detail = ""
	}
	isMessage := event.Type == "agent.message" || event.Type == "agent.output"
	if isMessage {
		message, _ := payload["message"].(string)
		detail = message
	}
	updated := row{id: id, event: event, summary: streaming.EventSummary(&event), detail: detail, kind: kind, status: status, message: isMessage}
	if idx, ok := m.rowIndex[id]; ok {
		updated.expanded = m.rows[idx].expanded
		if updated.detail == "" {
			updated.detail = m.rows[idx].detail
		}
		m.rows[idx] = updated
		if m.follow {
			m.moveToLatest()
		}
		return
	}
	if event.Type == "agent.activity" || event.Type == "agent.message" || event.Type == "agent.prompt" || event.Type == "agent.error" {
		m.rowIndex[id] = len(m.rows)
		m.rows = append(m.rows, updated)
		if m.follow {
			m.moveToLatest()
		}
	}
}

func (m *Model) moveToLatest() {
	visible := m.visibleRows()
	if len(visible) == 0 {
		m.cursor = 0
		return
	}
	m.cursor = len(visible) - 1
}

func (m Model) bodyHeight() int {
	if m.height <= 0 {
		return 20
	}
	height := m.height - 4
	if height < 1 {
		return 1
	}
	return height
}

func (m Model) viewportStart(visible []int, bodyHeight int) int {
	if len(visible) == 0 {
		return 0
	}
	cursor := m.cursor
	if cursor >= len(visible) {
		cursor = len(visible) - 1
	}
	used := 0
	start := cursor
	for start >= 0 {
		height := m.rowHeight(m.rows[visible[start]], bodyHeight)
		if used+height > bodyHeight && start < cursor {
			break
		}
		used += height
		start--
	}
	return start + 1
}

func (m Model) rowHeight(r row, bodyHeight int) int {
	if !r.expanded && !r.message {
		return 1
	}
	detail := r.detail
	if detail == "" {
		detail = "No additional detail."
	}
	maxDetail := minInt(18, bodyHeight-1)
	if r.message {
		maxDetail = len(strings.Split(strings.TrimSpace(detail), "\n"))
	}
	if maxDetail < 0 {
		maxDetail = 0
	}
	return 1 + len(indentDetail(detail, maxDetail))
}

func (m Model) rowBlock(r row, selected bool, bodyHeight int) []string {
	cursor := "  "
	if selected {
		cursor = "> "
	}
	fold := "+"
	if r.expanded {
		fold = "-"
	}
	line := fmt.Sprintf("%s[%s] %s", cursor, fold, r.summary)
	if r.message {
		line = cursor + "Agent message"
	}
	if selected {
		line = lipgloss.NewStyle().Bold(true).Render(line)
	}
	block := []string{line}
	if (r.expanded || r.message) && bodyHeight > 1 {
		detail := r.detail
		if detail == "" {
			detail = "No additional detail."
		}
		maxLines := minInt(18, bodyHeight-1)
		if r.message {
			maxLines = len(strings.Split(strings.TrimSpace(detail), "\n"))
		}
		block = append(block, indentDetail(detail, maxLines)...)
	}
	return block
}

func (m Model) visibleRows() []int {
	var out []int
	for i := range m.rows {
		if m.filter == "" || m.rows[i].kind == m.filter {
			out = append(out, i)
		}
	}
	return out
}

func toggleFilter(current, next string) string {
	if current == next {
		return ""
	}
	return next
}

func indentDetail(detail string, maxLines int) []string {
	if maxLines <= 0 {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(detail), "\n")
	if len(lines) > maxLines {
		remaining := len(lines) - maxLines
		lines = append(lines[:maxLines], fmt.Sprintf("... %d more lines", remaining))
	}
	for i := range lines {
		lines[i] = "      " + lines[i]
	}
	return lines
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
