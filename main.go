package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	gnet "github.com/shirou/gopsutil/v4/net"
	gprocess "github.com/shirou/gopsutil/v4/process"
)

const (
	stList = iota
	stLocked
	stFlying
	stExploding
	stResult
)

const (
	styleNormal = iota
	styleCursor
	styleLocked
	styleArmed
	styleBomb
	styleTrail
	styleFlash
	styleShock
	styleDebris
	styleEmber
	styleBorder
	styleTitle
	styleHint
	styleResult
)

var styles = map[int]lipgloss.Style{
	styleNormal:  lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
	styleCursor:  lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true),
	styleLocked:  lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true),
	styleArmed:   lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true),
	styleBomb:    lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Bold(true),
	styleTrail:   lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
	styleFlash:   lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(lipgloss.Color("196")).Bold(true),
	styleShock:   lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true),
	styleDebris:  lipgloss.NewStyle().Foreground(lipgloss.Color("208")),
	styleEmber:   lipgloss.NewStyle().Foreground(lipgloss.Color("238")),
	styleBorder:  lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	styleTitle:   lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true),
	styleHint:    lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	styleResult:  lipgloss.NewStyle().Foreground(lipgloss.Color("118")).Bold(true),
}

const (
	lockedTicks    = 14
	flyingTicks    = 28
	explodingTicks = 22
)

type PortInfo struct {
	Port int
	PID  int32
	Cmd  string
}

type tickMsg struct{}

type model struct {
	width, height int
	ports         []PortInfo
	cursor        int
	filter        string
	filtering     bool
	filterBuf     string
	state         int
	frame         int
	target        PortInfo
	targetIndex   int
	result        string
	searchMode    string
	notice        string
}

func main() {
	m := model{ports: getAllPorts()}
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("错误:", err)
	}
}

func tick() tea.Cmd {
	return tea.Tick(55*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} })
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.filtering {
			switch msg.String() {
			case "enter":
				m.filter = m.filterBuf
				m.filtering = false
				m.cursor = 0
				m.ports = listPorts(m.filter)
				m.notice = ""

				if strings.HasPrefix(m.filter, ":") {
					m.searchMode = "port"
					portStr := strings.TrimPrefix(m.filter, ":")
					if _, err := strconv.Atoi(portStr); err != nil {
						m.notice = "端口格式不对，示例：:8080"
					} else if len(m.ports) == 0 {
						m.notice = "端口模式：当前环境查不到端口，请改用进程名（如 sleep）"
					}
				} else if m.filter != "" {
					m.searchMode = "process"
					if len(m.ports) == 0 {
						m.notice = "进程模式：没有匹配的进程"
					}
				} else {
					m.searchMode = ""
				}
			case "esc":
				m.filtering = false
				m.filterBuf = ""
			case "backspace":
				if len(m.filterBuf) > 0 {
					m.filterBuf = m.filterBuf[:len(m.filterBuf)-1]
				}
			default:
				if len(msg.String()) == 1 {
					m.filterBuf += msg.String()
				}
			}
			return m, nil
		}

		switch m.state {
		case stList, stLocked:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.ports)-1 {
					m.cursor++
				}
			case "/":
				m.filtering = true
				m.filterBuf = m.filter
			case "enter":
				if len(m.ports) == 0 {
					break
				}
				if m.state == stList {
					m.target = m.ports[m.cursor]
					m.targetIndex = m.cursor
					m.state = stLocked
					m.frame = 0
					return m, tick()
				}
				m.state = stFlying
				m.frame = 0
				return m, tick()
			case "esc":
				if m.state == stLocked {
					m.state = stList
				}
			}

		case stResult:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "enter", "esc":
				m.state = stList
				m.notice = ""
				newPorts := listPorts(m.filter)
				if len(newPorts) == 0 && m.filter != "" {
					m.filter = ""
					m.filterBuf = ""
					m.searchMode = ""
					newPorts = getAllPorts()
					m.notice = "原筛选已无结果，已显示全部"
				}
				m.ports = newPorts
				if m.cursor >= len(m.ports) && m.cursor > 0 {
					m.cursor = len(m.ports) - 1
				}
			}
		}

	case tickMsg:
		m.frame++
		switch m.state {
		case stLocked:
			if m.frame >= lockedTicks {
				return m, nil
			}
			return m, tick()
		case stFlying:
			if m.frame >= flyingTicks {
				m.state = stExploding
				m.frame = 0
			}
			return m, tick()
		case stExploding:
			if m.frame >= explodingTicks {
				m.result = executeKill(m.target.PID)
				m.state = stResult
				m.frame = 0
				return m, nil
			}
			return m, tick()
		}
	}
	return m, nil
}

func bezier(p0x, p0y, p1x, p1y, p2x, p2y, t float64) (float64, float64) {
	u := 1 - t
	x := u*u*p0x + 2*u*t*p1x + t*t*p2x
	y := u*u*p0y + 2*u*t*p1y + t*t*p2y
	return x, y
}

func (m model) View() string {
	w, h := m.width, m.height
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}
	if w < 50 {
		w = 50
	}
	if h < 16 {
		h = 16
	}

	c := newCanvas(w, h)
	drawBorder(c)

	modeLabel := ""
	switch m.searchMode {
	case "port":
		modeLabel = " · 端口模式"
	case "process":
		modeLabel = " · 进程模式"
	}
	c.text(2, 0, " 端口处刑器"+modeLabel+" ", styleTitle)
	c.text(2, h-1, " ↑/↓ 选择  Enter 锁定/发射  / 搜索（:端口 或 进程名）  Esc 取消  q 退出 ", styleHint)

	listStartY := 2
	maxRows := h - 6
	if maxRows < 1 {
		maxRows = 1
	}

	offset := 0
	if m.cursor >= maxRows {
		offset = m.cursor - maxRows + 1
	}
	if offset > len(m.ports)-maxRows && len(m.ports) > maxRows {
		offset = len(m.ports) - maxRows
	}
	if offset < 0 {
		offset = 0
	}

	end := offset + maxRows
	if end > len(m.ports) {
		end = len(m.ports)
	}

	targetY := listStartY + (m.targetIndex - offset)
	targetX := w / 2

	for i := offset; i < end; i++ {
		p := m.ports[i]
		y := listStartY + (i - offset)
		cmd := p.Cmd
		maxLen := w - 24
		if maxLen < 0 {
			maxLen = 0
		}
		if len(cmd) > maxLen {
			cmd = cmd[:maxLen] + "..."
		}
		pidStr := "?"
		if p.PID > 0 {
			pidStr = strconv.Itoa(int(p.PID))
		}
		portStr := "   -   "
		if p.Port > 0 {
			portStr = fmt.Sprintf(":%-5d", p.Port)
		}
		line := fmt.Sprintf("%s  %-7s  %s", portStr, pidStr, cmd)

		sid := styleNormal
		prefix := "  "
		if i == m.cursor && m.state != stResult {
			sid = styleCursor
			prefix = "> "
		}
		c.text(2, y, prefix+line, sid)
	}

	if len(m.ports) == 0 {
		c.text(2, listStartY, "  (没有匹配的条目)", styleHint)
	}

	if m.state == stLocked {
		phases := []struct {
			txt   string
			style int
		}{
			{"[ LOCKED ]", styleLocked},
			{"[ LOCKED. ]", styleLocked},
			{"[ LOCKED.. ]", styleArmed},
			{"[ LOCKED... ]", styleArmed},
			{"[ ARMED! ]", styleArmed},
		}
		ph := phases[m.frame%len(phases)]
		if targetY >= 0 && targetY < h {
			drawTargetRow(c, 2, targetY, w, ph.txt, ph.style)
		}
		if m.frame > lockedTicks-4 {
			drawBorderColor(c, styleArmed)
		}
	}

	bombX := w - 12
	bombY := h - 4
	if m.state == stList || m.state == stLocked {
		pulse := m.frame / 6 % 2
		bs := styleBomb
		if pulse == 1 {
			bs = styleLocked
		}
		c.text(bombX, bombY, "[BOMB]", bs)
	}

	if m.state == stFlying {
		t := float64(m.frame) / float64(flyingTicks)
		ctrlX := float64(w) - 6
		ctrlY := 2.0
		bx, by := bezier(float64(bombX), float64(bombY), ctrlX, ctrlY, float64(targetX), float64(targetY), t)
		for k := 1; k <= 6; k++ {
			tt := t - float64(k)*0.035
			if tt < 0 {
				continue
			}
			tx, ty := bezier(float64(bombX), float64(bombY), ctrlX, ctrlY, float64(targetX), float64(targetY), tt)
			ch := []rune{'.', '.', '*', '+', '+', '*'}[k-1]
			st := styleTrail
			if k <= 2 {
				st = styleEmber
			}
			c.set(int(tx), int(ty), ch, st)
		}
		c.set(int(bx), int(by), '@', styleBomb)
		if t > 0.75 && targetY >= 0 && targetY < h {
			drawTargetRow(c, 2, targetY, w, "[ INCOMING ]", styleArmed)
		}
	}

	if m.state == stExploding {
		f := m.frame
		tx, ty := targetX, targetY

		if f <= 3 {
			for dy := -1; dy <= 1; dy++ {
				for dx := -2; dx <= 2; dx++ {
					c.set(tx+dx, ty+dy, '█', styleFlash)
				}
			}
		}

		if f >= 2 && f <= 12 {
			radius := float64(f) * 0.9
			steps := int(radius*6) + 12
			for i := 0; i < steps; i++ {
				ang := float64(i) / float64(steps) * 2 * math.Pi
				x := tx + int(radius*math.Cos(ang))
				y := ty + int(radius*math.Sin(ang)*0.6)
				if x >= 1 && x < w-1 && y >= 1 && y < h-1 {
					c.set(x, y, '*', styleShock)
				}
			}
		}

		if f >= 4 {
			debrisChars := []rune{'+', 'x', '*', '.', '\\', '/'}
			for i := 0; i < 14; i++ {
				ang := float64(i) * (2 * math.Pi / 14)
				dist := float64(f-4) * 0.9
				if dist > 14 {
					dist = 14
				}
				x := tx + int(dist*math.Cos(ang))
				y := ty + int(dist*math.Sin(ang)*0.55)
				if x >= 1 && x < w-1 && y >= 1 && y < h-1 {
					ch := debrisChars[i%len(debrisChars)]
					st := styleDebris
					if f > 12 {
						st = styleEmber
					}
					c.set(x, y, ch, st)
				}
			}
		}

		if f >= 14 && targetY >= 0 && targetY < h {
			burnt := strings.Repeat(" # ", (w-4)/3)
			c.text(2, targetY, burnt, styleEmber)
			for i := 0; i < 5; i++ {
				x := 3 + (i*7+f)%(w-6)
				c.set(x, targetY, []rune{'*', '+', '.', '`'}[i%4], styleDebris)
			}
		}
	}

	if m.filter != "" {
		c.text(w-20, 0, " 筛选:"+m.filter+" ", styleHint)
	}
	if m.filtering {
		c.text(2, h-2, " 🔍 "+m.filterBuf+"█", styleCursor)
	} else if m.notice != "" {
		c.text(2, h-2, " ⚠ "+m.notice, styleHint)
	}

	if m.state == stResult {
		msg := ">>> " + m.result + " <<<"
		c.text((w-len(msg))/2, h/2, msg, styleResult)
		hint := "Enter 返回  q 退出"
		c.text((w-len(hint))/2, h/2+2, hint, styleHint)
	}

	return c.render()
}

func drawTargetRow(c *canvas, x0, y, w int, label string, sid int) {
	if y < 0 || y >= c.h {
		return
	}
	start := x0 + 8
	for x := x0; x < w-2; x++ {
		c.set(x, y, ' ', styleNormal)
	}
	c.text(start, y, label, sid)
}

func drawBorderColor(c *canvas, sid int) {
	w, h := c.w, c.h
	for x := 1; x < w-1; x++ {
		c.cells[0][x].styleID = sid
		c.cells[h-1][x].styleID = sid
	}
	for y := 1; y < h-1; y++ {
		c.cells[y][0].styleID = sid
		c.cells[y][w-1].styleID = sid
	}
}

type cell struct {
	ch      rune
	styleID int
}

type canvas struct {
	w, h  int
	cells [][]cell
}

func newCanvas(w, h int) *canvas {
	cells := make([][]cell, h)
	for i := range cells {
		cells[i] = make([]cell, w)
		for j := range cells[i] {
			cells[i][j] = cell{' ', styleNormal}
		}
	}
	return &canvas{w, h, cells}
}

func (c *canvas) set(x, y int, r rune, sid int) {
	if x >= 0 && x < c.w && y >= 0 && y < c.h {
		c.cells[y][x] = cell{r, sid}
	}
}

func (c *canvas) text(x, y int, s string, sid int) {
	for i, r := range s {
		c.set(x+i, y, r, sid)
	}
}

func (c *canvas) render() string {
	var b strings.Builder
	for y := 0; y < c.h; y++ {
		x := 0
		for x < c.w {
			sid := c.cells[y][x].styleID
			start := x
			for x < c.w && c.cells[y][x].styleID == sid {
				x++
			}
			var sb strings.Builder
			for i := start; i < x; i++ {
				sb.WriteRune(c.cells[y][i].ch)
			}
			b.WriteString(styles[sid].Render(sb.String()))
		}
		if y < c.h-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func drawBorder(c *canvas) {
	w, h := c.w, c.h
	c.set(0, 0, '+', styleBorder)
	c.set(w-1, 0, '+', styleBorder)
	c.set(0, h-1, '+', styleBorder)
	c.set(w-1, h-1, '+', styleBorder)
	for x := 1; x < w-1; x++ {
		c.set(x, 0, '-', styleBorder)
		c.set(x, h-1, '-', styleBorder)
	}
	for y := 1; y < h-1; y++ {
		c.set(0, y, '|', styleBorder)
		c.set(w-1, y, '|', styleBorder)
	}
}

// ---------- 数据层 ----------

func getAllPorts() []PortInfo {
	result := listPortsByConnections()
	if len(result) == 0 {
		result = listProcessesByProc()
	}
	if len(result) == 0 {
		result = listProcessesByGopsutil()
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Port != result[j].Port {
			return result[i].Port < result[j].Port
		}
		return result[i].PID < result[j].PID
	})
	return result
}

func listPorts(filter string) []PortInfo {
	all := getAllPorts()
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return all
	}

	if strings.HasPrefix(filter, ":") {
		portStr := strings.TrimPrefix(filter, ":")
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil
		}
		var result []PortInfo
		for _, p := range all {
			if p.Port == port {
				result = append(result, p)
			}
		}
		return result
	}

	f := strings.ToLower(filter)
	var result []PortInfo
	for _, p := range all {
		if strings.Contains(strings.ToLower(p.Cmd), f) ||
			strings.Contains(strconv.Itoa(int(p.PID)), f) {
			result = append(result, p)
		}
	}
	return result
}

// 端口映射：桌面 Linux/macOS 有效；Android PRoot 通常返回空
func listPortsByConnections() []PortInfo {
	var result []PortInfo
	seen := make(map[int]bool)
	for _, proto := range []string{"tcp", "tcp6"} {
		conns, err := gnet.Connections(proto)
		if err != nil {
			continue
		}
		for _, c := range conns {
			if c.Status != "LISTEN" {
				continue
			}
			port := int(c.Laddr.Port)
			if port <= 0 || seen[port] {
				continue
			}
			seen[port] = true
			name := "?"
			if c.Pid > 0 {
				if p, err := gprocess.NewProcess(c.Pid); err == nil {
					if n, err := p.Name(); err == nil && n != "" {
						name = n
					}
				}
			}
			result = append(result, PortInfo{Port: port, PID: c.Pid, Cmd: name})
		}
	}
	return result
}

// 手写 /proc：Linux/PRoot 有效，Windows 返回空
func listProcessesByProc() []PortInfo {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var result []PortInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		parts := strings.Split(string(data), "\x00")
		var sb strings.Builder
		for _, p := range parts {
			if p == "" {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(p)
		}
		cmd := sb.String()
		if cmd == "" {
			continue
		}
		if len(cmd) > 80 {
			cmd = cmd[:80] + "..."
		}
		result = append(result, PortInfo{Port: 0, PID: int32(pid), Cmd: cmd})
	}
	return result
}

// gopsutil 进程兜底：Windows/macOS/Linux 都可用
func listProcessesByGopsutil() []PortInfo {
	procs, err := gprocess.Processes()
	if err != nil {
		return nil
	}
	var result []PortInfo
	for _, p := range procs {
		name, err := p.Name()
		if err != nil || name == "" {
			continue
		}
		if len(name) > 80 {
			name = name[:80] + "..."
		}
		result = append(result, PortInfo{Port: 0, PID: p.Pid, Cmd: name})
	}
	return result
}

func executeKill(pid int32) string {
	if pid <= 0 {
		return "无法处刑：没有 PID（权限不足或系统不支持）"
	}

	p, err := gprocess.NewProcess(pid)
	if err != nil {
		return "找不到进程: " + err.Error()
	}

	_ = p.Terminate()
	time.Sleep(80 * time.Millisecond)
	if running, _ := p.IsRunning(); !running {
		return "目标已被炸掉"
	}

	_ = p.Kill()
	time.Sleep(80 * time.Millisecond)
	if running, _ := p.IsRunning(); running {
		return "目标仍在，权限不足"
	}
	return "目标已被炸掉"
}
