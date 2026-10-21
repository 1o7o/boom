package main

import (
	"bufio"
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

const appVersion = "v0.3.2"

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
	styleHeader
	styleProtected
	styleHighlight
)

var styles = map[int]lipgloss.Style{
	styleNormal:    lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
	styleCursor:    lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true),
	styleLocked:    lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true),
	styleArmed:     lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true),
	styleBomb:      lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Bold(true),
	styleTrail:     lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
	styleFlash:     lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(lipgloss.Color("196")).Bold(true),
	styleShock:     lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true),
	styleDebris:    lipgloss.NewStyle().Foreground(lipgloss.Color("208")),
	styleEmber:     lipgloss.NewStyle().Foreground(lipgloss.Color("238")),
	styleBorder:    lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	styleTitle:     lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true),
	styleHint:      lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	styleResult:    lipgloss.NewStyle().Foreground(lipgloss.Color("118")).Bold(true),
	styleHeader:    lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Bold(true),
	styleProtected: lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true),
	styleHighlight: lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true),
}

const (
	lockedTicks    = 14
	flyingTicks    = 28
	explodingTicks = 22
	resultTicks    = 55
)

type PortInfo struct {
	Port int
	PID  int32
	Cmd  string
	Addr string
}

type tickMsg struct{}
type killDoneMsg struct{ result string }

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
	showAll       bool
	skin          int
	killPending   bool
	inContainer   bool
	unsafeMode    bool
}

// ================= main / CLI =================

func main() {
	args := os.Args[1:]

	var wantVersion, wantUnsafe bool
	var rest []string
	for _, a := range args {
		switch a {
		case "--version", "-v":
			wantVersion = true
		case "--unsafe":
			wantUnsafe = true
		default:
			rest = append(rest, a)
		}
	}

	if wantVersion {
		fmt.Println("boom " + appVersion)
		return
	}

	if len(rest) == 0 {
		m := model{
			ports:       getAllPorts(false),
			inContainer: inContainer(),
			unsafeMode:  wantUnsafe,
		}
		p := tea.NewProgram(m, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Println("错误:", err)
		}
		return
	}

	if rest[0] == "q" {
		runQMode(rest[1:], wantUnsafe)
		return
	}

	fmt.Fprintf(os.Stderr, "未知命令: %s\n", rest[0])
	fmt.Fprintln(os.Stderr, "试试: boom q")
	os.Exit(1)
}

func runQMode(args []string, unsafe bool) {
	var flagK, flagF, flagS, flagL bool
	var target string

	for _, a := range args {
		switch a {
		case "-k":
			flagK = true
		case "-f", "-y":
			flagF = true
		case "-s":
			flagS = true
		case "-l":
			flagL = true
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "未知参数: %s\n", a)
				os.Exit(1)
			}
			if target != "" {
				fmt.Fprintln(os.Stderr, "只能指定一个目标")
				os.Exit(1)
			}
			target = a
		}
	}

	if flagF && !flagK {
		fmt.Fprintln(os.Stderr, "-f / -y 必须配合 -k 使用")
		os.Exit(1)
	}
	if flagS && flagK {
		fmt.Fprintln(os.Stderr, "-s 和 -k 不能同时使用")
		os.Exit(1)
	}

	if !flagK && !flagS && !flagL {
		printQHelp()
		return
	}

	if flagL {
		all := getAllPorts(false)
		if len(all) == 0 {
			fmt.Println("(为空)")
			return
		}
		printMatches(all, false)
		return
	}

	if target == "" {
		fmt.Fprintln(os.Stderr, "缺少目标，例如 :8080 或 sleep")
		os.Exit(1)
	}

	matches := findMatches(target)
	if len(matches) == 0 {
		fmt.Printf("没有找到匹配 %q 的目标\n", target)
		os.Exit(1)
	}

	if flagS {
		printMatches(matches, true)
		return
	}

	var chosen PortInfo
	if len(matches) == 1 {
		chosen = matches[0]
	} else {
		printMatches(matches, true)
		chosen = pickByNumber(matches)
	}

	if chosen.PID == 1 && !unsafe {
		fmt.Println("PID 1 是容器/系统主进程，默认拒绝。加 --unsafe 才能处刑。")
		os.Exit(3)
	}

	if !flagF && !confirm(chosen) {
		fmt.Println("已取消")
		os.Exit(2)
	}

	result := executeKill(chosen.PID)
	fmt.Println(result)
	if strings.Contains(result, "已被炸掉") {
		os.Exit(0)
	}
	os.Exit(3)
}

func printQHelp() {
	fmt.Println(`boom - 端口/进程处刑器

用法:
  boom                         启动 TUI
  boom q -l                    列出所有监听端口
  boom q -s <目标>             搜索并列出
  boom q -k <目标>             杀目标（Y/N 确认）
  boom q -k -f <目标>          强杀，不确认（-y 是 -f 的别名）
  boom --version               显示版本
  boom --unsafe q -k <目标>    允许杀 PID 1

目标格式:
  :8080     按端口
  8080      先按端口，没找到再按 PID
  sleep     按进程名
  1234      按 PID

示例:
  boom q -s :8080
  boom q -k :8080
  boom q -k -f sleep
  boom q -k -y 1234

退出码:
  0  成功
  1  找不到匹配 / 参数错误
  2  用户取消
  3  权限不足 / PID 1 受保护`)
}

func printMatches(matches []PortInfo, numbered bool) {
	if numbered {
		fmt.Printf("%-4s %-8s %-16s %-7s %s\n", "NO", "PORT", "ADDR", "PID", "CMD")
	} else {
		fmt.Printf("%-8s %-16s %-7s %s\n", "PORT", "ADDR", "PID", "CMD")
	}
	for i, p := range matches {
		portStr := "-"
		if p.Port > 0 {
			portStr = fmt.Sprintf(":%d", p.Port)
		}
		addr := p.Addr
		if addr == "" {
			addr = "-"
		}
		if addr == "0.0.0.0" || addr == "::" {
			addr = "*"
		}
		if numbered {
			fmt.Printf("%-4d %-8s %-16s %-7d %s\n", i+1, portStr, addr, p.PID, p.Cmd)
		} else {
			fmt.Printf("%-8s %-16s %-7d %s\n", portStr, addr, p.PID, p.Cmd)
		}
	}
}

func pickByNumber(matches []PortInfo) PortInfo {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("输入序号（0 取消）: ")
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" || line == "0" || line == "q" || line == "Q" {
			fmt.Println("已取消")
			os.Exit(2)
		}
		n, err := strconv.Atoi(line)
		if err != nil || n < 1 || n > len(matches) {
			fmt.Println("无效序号，请重试")
			continue
		}
		return matches[n-1]
	}
}

func confirm(p PortInfo) bool {
	portStr := "-"
	if p.Port > 0 {
		portStr = fmt.Sprintf(":%d", p.Port)
	}
	addr := p.Addr
	if addr == "" {
		addr = "-"
	}
	if addr == "0.0.0.0" || addr == "::" {
		addr = "*"
	}
	fmt.Printf("Kill %s %s PID %d %s ? [Y/n] ", portStr, addr, p.PID, p.Cmd)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "" || line == "y" || line == "yes"
}

func findMatches(query string) []PortInfo {
	all := getAllPorts(false)
	query = strings.TrimSpace(query)

	if strings.HasPrefix(query, ":") {
		port, err := strconv.Atoi(strings.TrimPrefix(query, ":"))
		if err != nil {
			return nil
		}
		return dedupeByPID(filterByPort(all, port))
	}

	if n, err := strconv.Atoi(query); err == nil {
		byPort := filterByPort(all, n)
		if len(byPort) > 0 {
			return dedupeByPID(byPort)
		}
		var byPID []PortInfo
		for _, p := range all {
			if int(p.PID) == n {
				byPID = append(byPID, p)
			}
		}
		return dedupeByPID(byPID)
	}

	f := strings.ToLower(query)
	var r []PortInfo
	for _, p := range all {
		if strings.Contains(strings.ToLower(p.Cmd), f) {
			r = append(r, p)
		}
	}
	return dedupeByPID(r)
}

func filterByPort(all []PortInfo, port int) []PortInfo {
	var r []PortInfo
	for _, p := range all {
		if p.Port == port {
			r = append(r, p)
		}
	}
	return r
}

func dedupeByPID(procs []PortInfo) []PortInfo {
	seen := map[int32]int{}
	var result []PortInfo
	for _, p := range procs {
		if idx, ok := seen[p.PID]; ok {
			if result[idx].Addr == "" || result[idx].Addr == "0.0.0.0" || result[idx].Addr == "::" {
				result[idx].Addr = p.Addr
			}
			continue
		}
		seen[p.PID] = len(result)
		result = append(result, p)
	}
	return result
}

// ================= TUI =================

func inContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	data, err := os.ReadFile("/proc/1/cgroup")
	if err == nil {
		s := string(data)
		if strings.Contains(s, "docker") || strings.Contains(s, "containerd") || strings.Contains(s, "kubepods") {
			return true
		}
	}
	return false
}

func tick() tea.Cmd {
	return tea.Tick(55*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} })
}

func killCmd(pid int32) tea.Cmd {
	return func() tea.Msg {
		return killDoneMsg{result: executeKill(pid)}
	}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case killDoneMsg:
		m.result = msg.result
		m.killPending = false
		m.frame = 0
		return m, tick()

	case tea.KeyMsg:
		if m.filtering {
			switch msg.String() {
			case "enter":
				m.filter = m.filterBuf
				m.filtering = false
				m.cursor = 0
				m.ports = listPorts(m.filter, m.showAll)
				m.notice = ""
				if strings.HasPrefix(m.filter, ":") {
					m.searchMode = "port"
					portStr := strings.TrimPrefix(m.filter, ":")
					if _, err := strconv.Atoi(portStr); err != nil {
						m.notice = "端口格式不对，示例：:8080"
					} else if len(m.ports) == 0 {
						m.notice = "该端口没有找到占用进程"
					}
				} else if m.filter != "" {
					m.searchMode = "process"
					if len(m.ports) == 0 {
						m.notice = "没有找到匹配的进程"
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
		case stList:
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
			case "r":
				m.refresh()
				m.notice = "已刷新"
			case "a":
				m.showAll = !m.showAll
				m.ports = listPorts(m.filter, m.showAll)
				if m.cursor >= len(m.ports) && m.cursor > 0 {
					m.cursor = len(m.ports) - 1
				}
				if m.showAll {
					m.notice = "显示所有 TCP 连接"
				} else {
					m.notice = "只显示 LISTEN"
				}
			case "s":
				m.skin = (m.skin + 1) % 3
				names := []string{"导弹", "激光", "陨石"}
				m.notice = "皮肤: " + names[m.skin]
			case "enter":
				if len(m.ports) == 0 {
					break
				}
				cur := m.ports[m.cursor]
				if cur.PID == 1 && !m.unsafeMode {
					m.notice = "PID 1 受保护，加 --unsafe 才能处刑"
					break
				}
				m.target = cur
				m.targetIndex = m.cursor
				m.state = stLocked
				m.frame = 0
				m.notice = ""
				return m, tick()
			}

		case stLocked:
			// 锁定后不能移光标，只能确认或取消
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "enter", "y", "Y":
				m.state = stFlying
				m.frame = 0
				return m, tick()
			case "n", "N", "c", "C", "esc", "q", "0":
				m.state = stList
				m.notice = "已取消锁定"
				return m, nil
			}

		case stResult:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "enter", "esc":
				m.backToList()
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
				return m, tick()
			}
			return m, tick()
		case stExploding:
			if m.frame >= explodingTicks {
				m.state = stResult
				m.frame = 0
				m.killPending = true
				m.result = "发射中..."
				return m, killCmd(m.target.PID)
			}
			return m, tick()
		case stResult:
			if m.killPending {
				return m, nil
			}
			if m.frame >= resultTicks {
				m.backToList()
				return m, nil
			}
			return m, tick()
		}
	}
	return m, nil
}

func (m *model) refresh() {
	newPorts := listPorts(m.filter, m.showAll)
	var keepPID int32
	if m.cursor < len(m.ports) {
		keepPID = m.ports[m.cursor].PID
	}
	m.ports = newPorts
	if keepPID > 0 {
		for i, p := range m.ports {
			if p.PID == keepPID {
				m.cursor = i
				return
			}
		}
	}
	if m.cursor >= len(m.ports) && m.cursor > 0 {
		m.cursor = len(m.ports) - 1
	}
}

func (m *model) backToList() {
	m.state = stList
	m.notice = ""
	newPorts := listPorts(m.filter, m.showAll)
	if len(newPorts) == 0 && m.filter != "" {
		m.filter = ""
		m.filterBuf = ""
		m.searchMode = ""
		newPorts = getAllPorts(m.showAll)
		m.notice = "原筛选已无结果，已显示全部"
	}
	m.ports = newPorts
	if m.cursor >= len(m.ports) && m.cursor > 0 {
		m.cursor = len(m.ports) - 1
	}
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
	containerLabel := ""
	if m.inContainer {
		containerLabel = " [Container]"
	}
	title := " boom" + modeLabel + containerLabel + " "
	if len(title) < w-2 {
		c.text(2, 0, title, styleTitle)
	}

	var hint string
	switch m.state {
	case stList:
		hint = " ↑/↓ 选择  Enter 锁定  / 搜索  r 刷新  a 全部  s 皮肤  q 退出 "
	case stLocked:
		hint = " Enter/Y 发射   N/C/Esc/Q/0 取消锁定 "
	case stResult:
		hint = " Enter 返回  q 退出 "
	default:
		hint = ""
	}
	c.text(2, h-1, hint, styleHint)

	listStartY := 2
	maxRows := h - 7
	if maxRows < 1 {
		maxRows = 1
	}

	headerWide := w >= 78
	if headerWide {
		c.text(2, listStartY, fmt.Sprintf("%-8s %-16s %-7s %s", "PORT", "ADDR", "PID", "CMD"), styleHeader)
	} else {
		c.text(2, listStartY, fmt.Sprintf("%-8s %-7s %s", "PORT", "PID", "CMD"), styleHeader)
	}
	listStartY++
	maxRows--

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

	// 炸弹落点：目标行端口列右侧一点
	targetX := 16
	if targetX > w-10 {
		targetX = w - 10
	}
	targetY := listStartY + (m.targetIndex - offset)

	for i := offset; i < end; i++ {
		p := m.ports[i]
		y := listStartY + (i - offset)

		pidStr := "?"
		if p.PID > 0 {
			pidStr = strconv.Itoa(int(p.PID))
		}
		portStr := "   -   "
		if p.Port > 0 {
			portStr = fmt.Sprintf(":%-6d", p.Port)
		}

		var line string
		if headerWide {
			addr := p.Addr
			if addr == "" {
				addr = "-"
			}
			if addr == "0.0.0.0" || addr == "::" {
				addr = "*"
			}
			if len(addr) > 16 {
				addr = addr[:16]
			}
			maxCmd := w - 8 - 16 - 7 - 6
			if maxCmd < 5 {
				maxCmd = 5
			}
			cmd := p.Cmd
			if len(cmd) > maxCmd {
				cmd = cmd[:maxCmd] + "..."
			}
			line = fmt.Sprintf("%-8s %-16s %-7s %s", portStr, addr, pidStr, cmd)
		} else {
			maxCmd := w - 8 - 7 - 6
			if maxCmd < 5 {
				maxCmd = 5
			}
			cmd := p.Cmd
			if len(cmd) > maxCmd {
				cmd = cmd[:maxCmd] + "..."
			}
			line = fmt.Sprintf("%-8s %-7s %s", portStr, pidStr, cmd)
		}

		sid := styleNormal
		prefix := "  "
		if i == m.cursor && m.state != stResult {
			sid = styleCursor
			prefix = "> "
		}

		if p.PID == 1 {
			c.text(2, y, prefix+line, styleProtected)
			continue
		}

		if m.filter != "" && !m.filtering && m.searchMode == "process" {
			c.textHighlight(2+len(prefix), y, line, sid, m.filter)
		} else {
			c.text(2, y, prefix+line, sid)
		}
	}

	if len(m.ports) == 0 {
		c.text(2, listStartY, "  (为空)", styleHint)
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
		m.renderFlight(c, w, h, bombX, bombY, targetX, targetY)
		if m.progressF() > 0.75 && targetY >= 0 && targetY < h {
			drawTargetRow(c, 2, targetY, w, "[ INCOMING ]", styleArmed)
		}
	}

	if m.state == stExploding {
		m.renderExplosion(c, w, targetX, targetY)
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
		var hint2 string
		if m.killPending {
			hint2 = "处刑中..."
		} else {
			remain := (resultTicks - m.frame) * 55 / 1000
			if remain < 0 {
				remain = 0
			}
			hint2 = fmt.Sprintf("Enter 返回  q 退出  (%d 秒后自动返回)", remain)
		}
		c.text((w-len(hint2))/2, h/2+2, hint2, styleHint)
	}

	return c.render()
}

func (m model) progressF() float64 {
	t := float64(m.frame) / float64(flyingTicks)
	if t > 1 {
		t = 1
	}
	return t
}

func (m model) renderFlight(c *canvas, w, h, bombX, bombY, targetX, targetY int) {
	t := m.progressF()
	switch m.skin {
	case 1:
		bx := int(float64(bombX) + (float64(targetX)-float64(bombX))*t)
		by := int(float64(bombY) + (float64(targetY)-float64(bombY))*t)
		for k := 1; k <= 10; k++ {
			tt := t - float64(k)*0.03
			if tt < 0 {
				continue
			}
			tx := int(float64(bombX) + (float64(targetX)-float64(bombX))*tt)
			ty := int(float64(bombY) + (float64(targetY)-float64(bombY))*tt)
			c.set(tx, ty, '-', styleFlash)
		}
		c.set(bx, by, '=', styleBomb)
	case 2:
		startY := 1
		bx := targetX + m.frame%3 - 1
		by := int(float64(startY) + (float64(targetY)-float64(startY))*t)
		for k := 1; k <= 4; k++ {
			ty := by - k
			if ty > 0 {
				c.set(bx, ty, '*', styleTrail)
			}
		}
		c.set(bx, by, '@', styleDebris)
	default:
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
	}
}

func (m model) renderExplosion(c *canvas, w, targetX, targetY int) {
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
			if x >= 1 && x < w-1 && y >= 1 && y < c.h-1 {
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
			if x >= 1 && x < w-1 && y >= 1 && y < c.h-1 {
				ch := debrisChars[i%len(debrisChars)]
				st := styleDebris
				if f > 12 {
					st = styleEmber
				}
				c.set(x, y, ch, st)
			}
		}
	}

	if f >= 14 && targetY >= 0 && targetY < c.h {
		burnt := strings.Repeat(" # ", (w-4)/3)
		c.text(2, targetY, burnt, styleEmber)
		for i := 0; i < 5; i++ {
			x := 3 + (i*7+f)%(w-6)
			c.set(x, targetY, []rune{'*', '+', '.', '`'}[i%4], styleDebris)
		}
	}
}

func (c *canvas) textHighlight(x, y int, s string, baseSid int, keyword string) {
	if keyword == "" {
		c.text(x, y, s, baseSid)
		return
	}
	lower := strings.ToLower(s)
	kw := strings.ToLower(keyword)
	idx := strings.Index(lower, kw)
	if idx < 0 {
		c.text(x, y, s, baseSid)
		return
	}
	runes := []rune(s)
	idxR := len([]rune(s[:idx]))
	kwLen := len([]rune(keyword))
	c.text(x, y, string(runes[:idxR]), baseSid)
	c.text(x+idxR, y, string(runes[idxR:idxR+kwLen]), styleHighlight)
	c.text(x+idxR+kwLen, y, string(runes[idxR+kwLen:]), baseSid)
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

// ================= 数据层 =================

func getAllPorts(showAll bool) []PortInfo {
	result := listPortsByConnections(showAll)
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

func listPorts(filter string, showAll bool) []PortInfo {
	all := getAllPorts(showAll)
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

func listPortsByConnections(showAll bool) []PortInfo {
	var result []PortInfo
	seen := make(map[string]bool)
	for _, proto := range []string{"tcp", "tcp6"} {
		conns, err := gnet.Connections(proto)
		if err != nil {
			continue
		}
		for _, c := range conns {
			if !showAll && c.Status != "LISTEN" {
				continue
			}
			if showAll && c.Status != "LISTEN" && c.Status != "ESTABLISHED" {
				continue
			}
			port := int(c.Laddr.Port)
			if port <= 0 {
				continue
			}
			key := fmt.Sprintf("%s:%d:%d", c.Laddr.IP, port, c.Pid)
			if seen[key] {
				continue
			}
			seen[key] = true
			name := "?"
			if c.Pid > 0 {
				if p, err := gprocess.NewProcess(c.Pid); err == nil {
					if n, err := p.Name(); err == nil && n != "" {
						name = n
					}
				}
			}
			result = append(result, PortInfo{Port: port, PID: c.Pid, Cmd: name, Addr: c.Laddr.IP})
		}
	}
	return result
}

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
		return "无法处刑：没有 PID"
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
		return "目标仍在"
	}
	return "目标已被炸掉"
}
