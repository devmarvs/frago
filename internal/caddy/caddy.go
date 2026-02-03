package caddy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/devmarvs/frago/internal/port"
)

type Config struct {
	Path       string
	Port       int
	IsNew      bool
	BackupPath string
}

// ErrDesiredPortUnavailable indicates a requested port cannot be used.
var ErrDesiredPortUnavailable = errors.New("desired port unavailable")

// EnsureCaddyfile ensures a Caddyfile exists and uses a valid port.
// usedPorts is an optional set of ports already in use by the runner.Manager.
// desiredPort is optional; when set, it must be available or an error is returned.
// Returns a Config struct containing details about the operation.
func EnsureCaddyfile(dir string, usedPorts map[int]struct{}, desiredPort int) (*Config, error) {
	path := filepath.Join(dir, "Caddyfile")
	defaultStartPort := 8080
	defaultEndPort := 9000

	findFreePort := func(start, end int) (int, error) {
		for p := start; p <= end; p++ {
			if !port.IsPortFree(p) {
				continue
			}
			if usedPorts != nil {
				if _, exists := usedPorts[p]; exists {
					continue
				}
			}
			return p, nil
		}
		return 0, fmt.Errorf("no free port in range %d-%d", start, end)
	}
	checkDesiredPort := func(p int) error {
		if p <= 0 || p > 65535 {
			return fmt.Errorf("port %d is invalid; must be between 1 and 65535", p)
		}
		if !port.IsPortFree(p) {
			return fmt.Errorf("%w: port %d is already in use", ErrDesiredPortUnavailable, p)
		}
		if usedPorts != nil {
			if _, exists := usedPorts[p]; exists {
				return fmt.Errorf("%w: port %d is already used by another running project", ErrDesiredPortUnavailable, p)
			}
		}
		return nil
	}
	hasDesired := desiredPort > 0

	if hasDesired {
		if err := checkDesiredPort(desiredPort); err != nil {
			return nil, err
		}
	}

	// Check if exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Does not exist: find free port and create, avoiding usedPorts
		p := desiredPort
		if !hasDesired {
			var err error
			p, err = findFreePort(defaultStartPort, defaultEndPort)
			if err != nil {
				return nil, err
			}
		}

		content := fmt.Sprintf(":%d {\n\troot * .\n\tphp_server\n\tfile_server\n}", p)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return nil, err
		}
		return &Config{Path: path, Port: p, IsNew: true}, nil
	}

	// Exists: read it
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)

	lines := strings.Split(content, "\n")
	var (
		targetLine   int
		linePrefix   string
		lineSuffix   string
		lineFields   []string
		currentPort  int
		foundAddress bool
	)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			continue
		}

		braceIdx := strings.Index(trimmed, "{")
		beforeBrace := ""
		switch {
		case braceIdx >= 0:
			beforeBrace = strings.TrimSpace(trimmed[:braceIdx])
			lineSuffix = strings.TrimSpace(trimmed[braceIdx:])
		case i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == "{":
			beforeBrace = trimmed
			lineSuffix = ""
		default:
			continue
		}

		if beforeBrace == "" {
			continue
		}

		fields := strings.Fields(beforeBrace)
		for _, field := range fields {
			if _, p, ok := parseAddressPort(field); ok {
				currentPort = p
				lineFields = fields
				foundAddress = true
				break
			}
		}

		if foundAddress {
			targetLine = i
			linePrefix = line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			break
		}
	}

	if foundAddress {
		if hasDesired {
			if currentPort == desiredPort {
				return &Config{Path: path, Port: currentPort, IsNew: false}, nil
			}

			updated := false
			for i, field := range lineFields {
				prefix, p, ok := parseAddressPort(field)
				if ok && p == currentPort {
					lineFields[i] = formatAddress(prefix, desiredPort)
					updated = true
				}
			}
			if !updated {
				return nil, fmt.Errorf("could not update port in existing Caddyfile")
			}

			newLine := linePrefix + strings.Join(lineFields, " ")
			if lineSuffix != "" {
				newLine += " " + lineSuffix
			}
			lines[targetLine] = newLine
			newContent := strings.Join(lines, "\n")

			// Backup
			backupPath := path + ".bak"
			if err := os.WriteFile(backupPath, data, 0644); err != nil {
				return nil, err
			}

			// Write new
			if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
				return nil, err
			}

			return &Config{Path: path, Port: desiredPort, IsNew: false, BackupPath: backupPath}, nil
		}

		// Check if this port is free and not already used by another managed process
		if port.IsPortFree(currentPort) {
			if usedPorts != nil {
				if _, exists := usedPorts[currentPort]; exists {
					// Consider this port "in use" even if the OS thinks it's free
				} else {
					return &Config{Path: path, Port: currentPort, IsNew: false}, nil
				}
			} else {
				return &Config{Path: path, Port: currentPort, IsNew: false}, nil
			}
		}

		// Port occupied or already used by another managed process, need to replace
		// Find new free port, avoiding usedPorts
		newPort, err := findFreePort(defaultStartPort, defaultEndPort)
		if err != nil {
			return nil, err
		}

		updated := false
		for i, field := range lineFields {
			prefix, p, ok := parseAddressPort(field)
			if ok && p == currentPort {
				lineFields[i] = formatAddress(prefix, newPort)
				updated = true
			}
		}
		if !updated {
			return nil, fmt.Errorf("could not update port in existing Caddyfile")
		}

		newLine := linePrefix + strings.Join(lineFields, " ")
		if lineSuffix != "" {
			newLine += " " + lineSuffix
		}
		lines[targetLine] = newLine
		newContent := strings.Join(lines, "\n")

		// Backup
		backupPath := path + ".bak"
		if err := os.WriteFile(backupPath, data, 0644); err != nil {
			return nil, err
		}

		// Write new
		if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
			return nil, err
		}

		return &Config{Path: path, Port: newPort, IsNew: false, BackupPath: backupPath}, nil
	}

	return nil, fmt.Errorf("could not find port definition in existing Caddyfile")
}

// EnsureCaddyfileAutoPort prefers desiredPort when available and falls back to any free port otherwise.
func EnsureCaddyfileAutoPort(dir string, usedPorts map[int]struct{}, desiredPort int) (*Config, error) {
	cfg, err := EnsureCaddyfile(dir, usedPorts, desiredPort)
	if err == nil {
		return cfg, nil
	}

	if desiredPort > 0 && errors.Is(err, ErrDesiredPortUnavailable) {
		return EnsureCaddyfile(dir, usedPorts, 0)
	}

	return nil, err
}

func RemoveCaddyfile(dir string) error {
	path := filepath.Join(dir, "Caddyfile")
	backup := path + ".bak"
	return errors.Join(removeIfExists(path), removeIfExists(backup))
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func parseAddressPort(addr string) (string, int, bool) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", 0, false
	}

	if strings.HasPrefix(addr, ":") {
		portStr := strings.TrimPrefix(addr, ":")
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return "", 0, false
		}
		return ":", port, true
	}

	if strings.HasPrefix(addr, "[") {
		idx := strings.LastIndex(addr, "]:")
		if idx == -1 {
			return "", 0, false
		}
		host := addr[:idx+1]
		portStr := addr[idx+2:]
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return "", 0, false
		}
		return host, port, true
	}

	idx := strings.LastIndex(addr, ":")
	if idx == -1 {
		return "", 0, false
	}
	host := addr[:idx]
	if host == "" {
		return "", 0, false
	}
	portStr := addr[idx+1:]
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, false
	}
	return host, port, true
}

func formatAddress(prefix string, port int) string {
	if prefix == ":" {
		return fmt.Sprintf(":%d", port)
	}
	return fmt.Sprintf("%s:%d", prefix, port)
}
