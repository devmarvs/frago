package runner

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/devmarvs/frago/internal/caddy"
)

// Process represents a running FrankenPHP instance.
type Process struct {
	ID           string
	Cmd          *exec.Cmd
	URL          string
	Port         int
	ProjectPath  string
	CaddyConfig  *caddy.Config
	BinaryPath   string
	VersionLabel string
	StartedAt    time.Time
}

type ExitInfo struct {
	When   time.Time
	Err    string
	Failed bool
}

// Manager handles multiple FrankenPHP process states.
type Manager struct {
	mu        sync.Mutex
	processes map[string]*Process
	logs      map[string]*LogBuffer
	exitInfo  map[string]ExitInfo
	stopReq   map[string]bool
}

// NewManager creates a new process manager.
func NewManager() *Manager {
	return &Manager{
		processes: make(map[string]*Process),
		logs:      make(map[string]*LogBuffer),
		exitInfo:  make(map[string]ExitInfo),
		stopReq:   make(map[string]bool),
	}
}

func DefaultFrankenPHPBinary() string {
	if env := os.Getenv("FRANKENPHP_BINARY"); env != "" {
		return env
	}

	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)

		candidates := []string{
			filepath.Join(exeDir, "frankenphp"),
		}

		if runtime.GOOS == "windows" {
			candidates = append(candidates, filepath.Join(exeDir, "frankenphp.exe"))
		}

		for _, p := range candidates {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}

	return "frankenphp"
}

// Start launches FrankenPHP in the given directory.
func (m *Manager) Start(dir string, config *caddy.Config, binaryPath string, versionLabel string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already running for this directory
	if _, exists := m.processes[dir]; exists {
		return fmt.Errorf("process already running for directory: %s", dir)
	}

	delete(m.exitInfo, dir)
	delete(m.stopReq, dir)

	selectedBinary := binaryPath
	if binaryPath == "" {
		binaryPath = DefaultFrankenPHPBinary()
	}
	displayLabel := FormatVersionLabel(selectedBinary, versionLabel)

	logBuffer := m.getOrCreateLogBufferLocked(dir)

	cmd := exec.Command(binaryPath, "run", "--config", config.Path)
	cmd.Dir = dir
	cmd.Stdout = io.MultiWriter(os.Stdout, logBuffer)
	cmd.Stderr = io.MultiWriter(os.Stderr, logBuffer)

	if err := cmd.Start(); err != nil {
		return err
	}

	proc := &Process{
		ID:           dir,
		Cmd:          cmd,
		URL:          fmt.Sprintf("http://localhost:%d", config.Port),
		Port:         config.Port,
		ProjectPath:  dir,
		CaddyConfig:  config,
		BinaryPath:   selectedBinary,
		VersionLabel: displayLabel,
		StartedAt:    time.Now(),
	}
	m.processes[dir] = proc

	// Monitor in background
	go func(p *Process) {
		err := p.Cmd.Wait()
		m.recordExit(p.ID, err)
		m.cleanup(p.ID)
	}(proc)

	return nil
}

// cleanup removes the process from the map and cleans up Caddyfile.
func (m *Manager) cleanup(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	proc, exists := m.processes[id]
	if !exists {
		return
	}

	// Clean up Caddyfile
	if proc.CaddyConfig != nil {
		if proc.CaddyConfig.IsNew {
			// It was a new file, delete it
			_ = os.Remove(proc.CaddyConfig.Path)
		} else if proc.CaddyConfig.BackupPath != "" {
			// It was modified, restore backup
			_ = os.Rename(proc.CaddyConfig.BackupPath, proc.CaddyConfig.Path)
		}
	}

	delete(m.processes, id)
}

func (m *Manager) recordExit(id string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stopRequested := m.stopReq[id]
	if stopRequested {
		delete(m.stopReq, id)
	}

	msg := ""
	if err != nil {
		msg = err.Error()
	}

	m.exitInfo[id] = ExitInfo{
		When:   time.Now(),
		Err:    msg,
		Failed: err != nil && !stopRequested,
	}
}

func (m *Manager) LastExit(dir string) (ExitInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.exitInfo[dir]
	return info, ok
}

func (m *Manager) ClearExit(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.exitInfo, dir)
	delete(m.stopReq, dir)
}

// Stop terminates the running process for a specific directory.
func (m *Manager) Stop(dir string) error {
	m.mu.Lock()
	proc, exists := m.processes[dir]
	if exists {
		m.stopReq[dir] = true
	}
	// Unlock before killing to avoid deadlock in cleanup (which also locks)
	// Actually, Start() creates a goroutine that waits.
	// If we kill, wait returns, cleanup is called.
	// cleanup locks.
	// So we should unlock here.
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("no process running for directory: %s", dir)
	}

	if proc.Cmd.Process != nil {
		if err := proc.Cmd.Process.Kill(); err != nil {
			m.mu.Lock()
			delete(m.stopReq, dir)
			m.mu.Unlock()
			return err
		}
	}

	return nil
}

// List returns a list of running processes.
func (m *Manager) List() []*Process {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := make([]*Process, 0, len(m.processes))
	for _, p := range m.processes {
		list = append(list, p)
	}
	return list
}

// UsedPorts returns a set of ports currently in use by managed processes.
func (m *Manager) UsedPorts() map[int]struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()

	used := make(map[int]struct{}, len(m.processes))
	for _, p := range m.processes {
		used[p.Port] = struct{}{}
	}
	return used
}

// Get returns a specific process.
func (m *Manager) Get(dir string) (*Process, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.processes[dir]
	return p, ok
}

func OpenBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	default: // "linux", "freebsd", "openbsd", "netbsd"
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}
