package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type PHPVersion struct {
	Path    string `json:"path"`
	Version string `json:"version"` // e.g., "8.2.1"
	Label   string `json:"label"`   // e.g., "FrankenPHP (PHP 8.2.1)"
}

const versionCommandTimeout = 2 * time.Second

// DetectVersions scans PATH for frankenphp binaries and extracts their PHP versions.
func DetectVersions() ([]PHPVersion, error) {
	pathEnv := os.Getenv("PATH")
	paths := strings.Split(pathEnv, string(os.PathListSeparator))

	uniquePaths := make(map[string]bool)
	var versions []PHPVersion

	versionRegex := regexp.MustCompile(`PHP (\d+\.\d+\.\d+)`)

	addCandidate := func(name, fullPath string) {
		realPath, err := filepath.EvalSymlinks(fullPath)
		if err != nil {
			realPath = fullPath
		}

		if uniquePaths[realPath] {
			return
		}
		uniquePaths[realPath] = true

		out, err := runVersionCommand(fullPath)
		if err != nil {
			return
		}

		matches := versionRegex.FindStringSubmatch(string(out))
		phpVer := "Unknown"
		if len(matches) > 1 {
			phpVer = matches[1]
		}

		versions = append(versions, PHPVersion{
			Path:    fullPath,
			Version: phpVer,
			Label:   fmt.Sprintf("%s (PHP %s)", name, phpVer),
		})
	}

	for _, dir := range paths {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(strings.ToLower(name), "frankenphp") {
				fullPath := filepath.Join(dir, name)
				addCandidate(name, fullPath)
			}
		}
	}

	bundled := DefaultFrankenPHPBinary()
	if bundled != "" {
		_, name := filepath.Split(bundled)
		addCandidate(name, bundled)
	}

	return versions, nil
}

// GetBinaryVersion returns the PHP version string for a given FrankenPHP binary path.
func GetBinaryVersion(path string) (string, error) {
	out, err := runVersionCommand(path)
	if err != nil {
		return "", err
	}

	versionRegex := regexp.MustCompile(`PHP (\d+\.\d+\.\d+)`)
	matches := versionRegex.FindStringSubmatch(string(out))
	if len(matches) > 1 {
		return matches[1], nil
	}
	return "Unknown", fmt.Errorf("could not parse version")
}

// FormatVersionLabel returns a user-facing label for a FrankenPHP binary.
func FormatVersionLabel(binaryPath string, fallbackLabel string) string {
	if fallbackLabel != "" {
		return fallbackLabel
	}

	label := "Default (System Path)"
	versionPath := binaryPath
	if binaryPath == "" {
		versionPath = DefaultFrankenPHPBinary()
	} else {
		label = filepath.Base(binaryPath)
	}

	if versionPath == "" {
		return label
	}

	phpVer, err := GetBinaryVersion(versionPath)
	if err != nil {
		return label
	}

	return fmt.Sprintf("%s (PHP %s)", label, phpVer)
}

// GetFrankenPHPVersion returns the FrankenPHP version string (e.g. "v1.1.0").
func GetFrankenPHPVersion(path string) (string, error) {
	out, err := runVersionCommand(path)
	if err != nil {
		return "", err
	}

	// Example output: "FrankenPHP v1.1.0 PHP 8.3.3 Caddy v2.7.6 ..."
	versionRegex := regexp.MustCompile(`FrankenPHP (v\d+\.\d+\.\d+)`)
	matches := versionRegex.FindStringSubmatch(string(out))
	if len(matches) > 1 {
		return matches[1], nil
	}
	return "", fmt.Errorf("could not parse FrankenPHP version")
}

func runVersionCommand(path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), versionCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "--version")
	return cmd.Output()
}
