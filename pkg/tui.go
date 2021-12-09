package butt

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	listWidth    = 14
	headerHeight = 2
	footerHeight = 2
)

type item struct {
	title, desc, jobName string
}

func (j *JobSpec) GetTitle() string {
	if len(j.runs) > 0 && j.runs[0].Status != 0 {
		return j.Name + " " + lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")).Bold(true).Render("!")
	}
	return j.Name
}

func (j *JobSpec) GetStatusDescription() string {
	if len(j.runs) == 0 {
		return ""
	}

	lastRun := j.runs[0]
	var sb strings.Builder

	since := time.Since(lastRun.TriggeredAt).String()
	sb.WriteString("ran " + since + " ago")

	if lastRun.Status != 0 {
		sb.WriteString(" | ERROR")
	}

	return sb.String()

}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

type model struct {
	list          list.Model
	state         *Schedule
	choice        string
	quitting      bool
	width         int
	height        int
	ready         bool
	listFocus     bool
	viewportFocus bool
	viewport      viewport.Model
}

func (j *JobSpec) RunInfo() string {
	// spew.Dump(j.runs[0].Status)
	var runInfo string
	if len(j.runs) == 0 {
		runInfo = "no run history"
	} else if j.runs[0].Status == 0 {
		since := time.Since(j.runs[0].TriggeredAt).String()
		runInfo = "ran " + since + " ago"

	} else {
		runInfo += lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")).Render("error'd")

	}

	return runInfo

}

func (j *JobSpec) View() string {

	var sb strings.Builder

	if len(j.runs) == 0 {
		sb.WriteString("no run history")
		return sb.String()
	}

	for _, jr := range j.runs {
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render(jr.TriggeredAt.String()))
		sb.WriteString("\n")
		sb.WriteString(jr.Log)
		sb.WriteString("\n\n")

	}

	return sb.String()

}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// reset viewport state, view will run before this
	// m.viewportDirty = false

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width

		m.viewport = viewport.Model{Width: msg.Width - listWidth, Height: msg.Height - headerHeight - footerHeight - 3}
		if !m.ready {
			m.viewport.SetContent("")
			m.ready = true
		}

		m.list.SetHeight(msg.Height - footerHeight - headerHeight)

	case tea.KeyMsg:
		switch keypress := msg.String(); keypress {
		case "left":
			m.listFocus = true
			m.viewportFocus = !m.listFocus
		case "right":
			m.viewportFocus = true
			m.listFocus = !m.viewportFocus
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "enter":
			i, ok := m.list.SelectedItem().(item)
			if ok {
				if i.jobName != m.choice {
					// m.ready = false
					m.choice = i.jobName
					j := m.state.Jobs[m.choice]
					m.viewport.SetContent(j.View())
				}

			}
		}
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd

	if m.viewportFocus {
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}
	if m.listFocus {
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)

}

func (m model) View() string {
	var j JobSpec
	if _, ok := m.state.Jobs[m.choice]; ok {
		j = *m.state.Jobs[m.choice]
	} else {
		j = JobSpec{}
	}

	title := lipgloss.NewStyle().Width(m.width).Foreground(lipgloss.Color("#49f770")).Bold(true).Render("butt: Better Unified Time-Driven Triggers")

	jobListStyle := lipgloss.NewStyle().Border(lipgloss.NormalBorder())

	if m.listFocus {
		jobListStyle = jobListStyle.BorderForeground(lipgloss.Color("228"))
	}

	jobList := jobListStyle.Render(m.list.View())

	jobTitle := lipgloss.NewStyle().Foreground(lipgloss.Color("#49f770")).Bold(true).Render(j.Name)
	jobStatus := lipgloss.NewStyle().Faint(true).Align(lipgloss.Right).PaddingRight(1).Width(m.width - lipgloss.Width(jobTitle) - lipgloss.Width(jobList) - 4).Render(j.RunInfo())

	headerBorder := lipgloss.Border{
		Bottom: "_.-.",
	}
	header := lipgloss.NewStyle().Border(headerBorder).BorderTop(false).MarginBottom(1).Render(lipgloss.JoinHorizontal(lipgloss.Left, jobTitle, jobStatus))

	hx := lipgloss.NewStyle().Faint(true).Align(lipgloss.Right).Render(Hex.Poke())

	// job view
	vpBox := lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1).Render(m.viewport.View())

	// job box
	jobBox := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Render(
		lipgloss.JoinVertical(lipgloss.Left, header, vpBox))

	mv := lipgloss.JoinVertical(lipgloss.Left, title, lipgloss.JoinHorizontal(lipgloss.Top, jobList, jobBox), hx)

	return mv
}

func (s *Schedule) GetSchedule() error {
	// addr should be configurable
	r, err := http.Get("http://localhost:8081/schedule")
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return json.NewDecoder(r.Body).Decode(s)
}

func TUI() {
	// init schedule schedule
	schedule := &Schedule{}
	if err := schedule.GetSchedule(); err != nil {
		fmt.Printf("Error connecting with butt server: %v\n", err.Error())
		os.Exit(1)
	}

	items := []list.Item{}
	for _, v := range schedule.Jobs {
		v.LoadRuns()
		item := item{title: v.GetTitle(), jobName: v.Name}
		items = append(items, item)
		// get run history for each job
	}

	id := list.NewDefaultDelegate()
	id.ShowDescription = false
	id.SetSpacing(0)

	l := list.NewModel(items, id, listWidth, 10)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.SetShowTitle(false)

	m := model{list: l, state: schedule, listFocus: true}
	// if len(items) > 0 {
	// 	m.choice = items[len(items)-1].(item).jobName
	// }

	if err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Start(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
