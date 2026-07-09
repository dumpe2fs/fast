package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Phase int

const (
	PhaseInit Phase = iota
	PhaseDownload
	PhaseUpload
	PhaseDone
)

const (
	connections = 5
	duration    = 10 * time.Second
	sparkWidth  = 20
)

const (
	accentColor   = "#2EF8BB" // Mint green for Download
	ulAccentColor = "#B282FF" // Purple for Upload
)

var (
	labelStyle   = lipgloss.NewStyle().Bold(true).Width(12)
	speedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(accentColor)).Bold(true)
	unitStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	dlSparkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(accentColor))
	ulSparkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ulAccentColor))
	peakStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	baseStyle    = lipgloss.NewStyle().Padding(1, 2)
)

const tickInterval = time.Second / 10

type tickMsg time.Time

func tickCmd(t time.Time) tea.Msg {
	return tickMsg(t)
}

type Metric struct {
	bytes     *atomic.Int64
	lastBytes int64
	lastTick  time.Time
	speed     float64
	speeds    []float64
	peak      float64
}

type Model struct {
	targets []string

	dl         Metric
	ul         Metric
	idlePing   *atomic.Int64
	loadedPing *atomic.Int64
	dns        *atomic.Int64

	ctxDl    context.Context
	cancelDl context.CancelFunc
	ctxUl    context.Context
	cancelUl context.CancelFunc

	phase      Phase
	phaseStart time.Time

	done     bool
	quitting bool
}

func NewModel(targets []string) Model {
	ctxDl, cancelDl := context.WithCancel(context.Background())
	ctxUl, cancelUl := context.WithCancel(context.Background())

	now := time.Now()
	return Model{
		targets:    targets,
		dl:         Metric{bytes: &atomic.Int64{}, lastTick: now},
		ul:         Metric{bytes: &atomic.Int64{}},
		idlePing:   &atomic.Int64{},
		loadedPing: &atomic.Int64{},
		dns:        &atomic.Int64{},
		ctxDl:      ctxDl,
		cancelDl:   cancelDl,
		ctxUl:      ctxUl,
		cancelUl:   cancelUl,
		phase:      PhaseDownload,
		phaseStart: now,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tea.Tick(tickInterval, tickCmd), m.measure)
}

func (m Model) measure() tea.Msg {
	go func() {
		if len(m.targets) > 0 {
			target := m.targets[0]
			if d, err := measurePing(target); err == nil {
				m.idlePing.Store(int64(d))
			}
		}

		// Start only Download initially
		for _, url := range m.targets {
			go download(m.ctxDl, url, m.dl.bytes)
		}

		// Continuously measure Loaded Ping
		if len(m.targets) > 0 {
			target := m.targets[0]
			// Run loaded ping as long as either phase is active
			for m.ctxDl.Err() == nil || m.ctxUl.Err() == nil {
				time.Sleep(500 * time.Millisecond)
				if d, err := measurePing(target); err == nil {
					m.loadedPing.Store(int64(d))
				}
			}
		}
	}()

	// Measure DNS concurrently
	go func() {
		d := measureDNS()
		m.dns.Store(int64(d))
	}()

	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			m.cancelDl()
			m.cancelUl()
			return m, tea.Quit
		}

	case tickMsg:
		now := time.Time(msg)
		elapsed := now.Sub(m.phaseStart)

		if m.phase == PhaseDownload {
			currentBytes := m.dl.bytes.Load()
			deltaBytes := currentBytes - m.dl.lastBytes
			actualElapsed := now.Sub(m.dl.lastTick)
			m.dl.lastBytes = currentBytes
			m.dl.lastTick = now
			m.dl.speed = mbps(deltaBytes, actualElapsed)
			if m.dl.speed > m.dl.peak {
				m.dl.peak = m.dl.speed
			}
			m.dl.speeds = append(m.dl.speeds, m.dl.speed)

			if elapsed >= duration {
				m.cancelDl()
				m.phase = PhaseUpload
				
				nowUpload := time.Now()
				m.phaseStart = nowUpload
				m.ul.lastTick = nowUpload
				// Start Upload
				for _, url := range m.targets {
					go upload(m.ctxUl, url, m.ul.bytes)
				}
			}
		} else if m.phase == PhaseUpload {
			currentBytes := m.ul.bytes.Load()
			deltaBytes := currentBytes - m.ul.lastBytes
			actualElapsed := now.Sub(m.ul.lastTick)
			m.ul.lastBytes = currentBytes
			m.ul.lastTick = now
			m.ul.speed = mbps(deltaBytes, actualElapsed)
			if m.ul.speed > m.ul.peak {
				m.ul.peak = m.ul.speed
			}
			m.ul.speeds = append(m.ul.speeds, m.ul.speed)

			if elapsed >= duration {
				m.done = true
				m.phase = PhaseDone
				m.cancelUl()
				return m, tea.Quit
			}
		}

		return m, tea.Tick(tickInterval, tickCmd)
	}

	return m, nil
}

func renderMetric(label string, metric Metric, sparkStyle lipgloss.Style, isPending bool) string {
	var s strings.Builder
	s.WriteString(labelStyle.Render(label))

	if isPending && len(metric.speeds) == 0 {
		s.WriteString(unitStyle.Render(" Pending..."))
		return s.String()
	}

	speed, unit := scale(metric.speed)
	s.WriteString(speedStyle.Render(fmt.Sprintf("%5.1f", speed)))
	s.WriteString(unitStyle.Render(" " + unit))
	s.WriteString(" ")
	s.WriteString(sparkStyle.Render(sparkline(metric.speeds, metric.peak, sparkWidth)))
	if metric.peak > 0 {
		peak, peakUnit := scale(metric.peak)
		peakLabel := fmt.Sprintf("  peak %.0f", peak)
		if peakUnit != unit {
			peakLabel += " " + peakUnit
		}
		s.WriteString(peakStyle.Render(peakLabel))
	}
	return s.String()
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var s strings.Builder

	idle := m.idlePing.Load()
	loaded := m.loadedPing.Load()
	pingStr := ""
	if idle == 0 {
		pingStr = unitStyle.Render(" Pending...") + "\n\n"
	} else if loaded == 0 {
		pingStr = " " + speedStyle.Render(fmt.Sprintf("%d", idle/1e6)) + unitStyle.Render(" ms (idle) / ") + unitStyle.Render("Pending...") + "\n\n"
	} else {
		pingStr = " " + speedStyle.Render(fmt.Sprintf("%d", idle/1e6)) + unitStyle.Render(" ms (idle) / ") +
			speedStyle.Render(fmt.Sprintf("%d", loaded/1e6)) + unitStyle.Render(" ms (loaded)") + "\n\n"
	}

	s.WriteString(renderMetric("Download:", m.dl, dlSparkStyle, false) + "\n\n")
	s.WriteString(renderMetric("Upload:", m.ul, ulSparkStyle, m.phase == PhaseDownload) + "\n\n")
	s.WriteString(labelStyle.Render("Ping:") + pingStr)

	dnsTime := m.dns.Load()
	dnsStr := ""
	if dnsTime == 0 {
		dnsStr = unitStyle.Render(" Pending...") + "\n"
	} else {
		dnsStr = " " + speedStyle.Render(fmt.Sprintf("%d", dnsTime/1e6)) + unitStyle.Render(" ms") + "\n"
	}
	s.WriteString(labelStyle.Render("DNS Time:") + dnsStr)

	style := baseStyle
	if m.done {
		style = style.PaddingBottom(2)
	}
	return style.Render(s.String())
}

// mbps converts a number of bytes downloaded over a duration into megabits per
// second, the unit fast.com reports.
func mbps(bytes int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(bytes) * 8 / d.Seconds() / 1e6
}

// scale converts a speed in Mbps to its display magnitude and unit, switching to
// Gbps once it would read past 999.9 Mbps so the value never exceeds "999.9".
func scale(speed float64) (float64, string) {
	if speed >= 999.95 {
		return speed / 1000, "Gbps"
	}
	return speed, "Mbps"
}

func main() {
	urls, err := targets(connections)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) {
			fmt.Fprintln(os.Stderr, "No internet connection.")
			os.Exit(1)
		}
		log.Fatal(err)
	}

	if len(urls) == 0 {
		fmt.Fprintln(os.Stderr, "No fast.com servers available.")
		os.Exit(1)
	}

	if _, err := tea.NewProgram(NewModel(urls)).Run(); err != nil {
		log.Fatal(err)
	}
}
