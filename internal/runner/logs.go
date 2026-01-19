package runner

import (
	"strings"
	"sync"
)

const defaultLogMaxLines = 1000

type LogBuffer struct {
	mu      sync.Mutex
	lines   []string
	max     int
	partial string
}

func NewLogBuffer(max int) *LogBuffer {
	if max <= 0 {
		max = defaultLogMaxLines
	}
	return &LogBuffer{max: max}
}

func (b *LogBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	text := strings.ReplaceAll(string(p), "\r\n", "\n")

	b.mu.Lock()
	defer b.mu.Unlock()

	text = b.partial + text
	b.partial = ""

	parts := strings.Split(text, "\n")
	if len(parts) == 0 {
		return len(p), nil
	}

	if !strings.HasSuffix(text, "\n") {
		b.partial = parts[len(parts)-1]
		parts = parts[:len(parts)-1]
	}

	for _, line := range parts {
		b.appendLine(line)
	}

	return len(p), nil
}

func (b *LogBuffer) appendLine(line string) {
	b.lines = append(b.lines, line)
	if len(b.lines) > b.max {
		b.lines = b.lines[len(b.lines)-b.max:]
	}
}

func (b *LogBuffer) Tail(n int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	lines := make([]string, 0, len(b.lines)+1)
	lines = append(lines, b.lines...)
	if b.partial != "" {
		lines = append(lines, b.partial)
	}

	if n <= 0 || n >= len(lines) {
		out := make([]string, len(lines))
		copy(out, lines)
		return out
	}

	out := make([]string, n)
	copy(out, lines[len(lines)-n:])
	return out
}

func (b *LogBuffer) TailText(n int) string {
	return strings.Join(b.Tail(n), "\n")
}

func (m *Manager) getOrCreateLogBufferLocked(dir string) *LogBuffer {
	if m.logs == nil {
		m.logs = make(map[string]*LogBuffer)
	}
	buf := m.logs[dir]
	if buf == nil {
		buf = NewLogBuffer(defaultLogMaxLines)
		m.logs[dir] = buf
	}
	return buf
}

func (m *Manager) TailLogs(dir string, n int) string {
	m.mu.Lock()
	buf := m.logs[dir]
	m.mu.Unlock()
	if buf == nil {
		return ""
	}
	return buf.TailText(n)
}

func (m *Manager) ClearLogs(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.logs, dir)
}
