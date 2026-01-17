package updater

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/devmarvs/frago/internal/runner"
)

type ReleaseInfo struct {
	TagName string `json:"tag_name"`
	HtmlUrl string `json:"html_url"`
	Body    string `json:"body"`
}

// CheckForUpdates compares the local FrankenPHP version with the latest GitHub release.
// Returns the release info if an update is available, or nil if up-to-date.
func CheckForUpdates() (*ReleaseInfo, error) {
	// 1. Get local version
	binaryPath := runner.DefaultFrankenPHPBinary()
	
	// 2. Fetch latest release from GitHub
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/php/frankenphp/releases/latest")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status: %s", resp.Status)
	}

	var release ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode release info: %w", err)
	}

	// 3. Compare versions
	// FrankenPHP version from binary (e.g., "v1.1.0")
	localFrankenVer, err := runner.GetFrankenPHPVersion(binaryPath)
	if err != nil {
		// Fallback or treat as update available
		return &release, nil 
	}

	if localFrankenVer != release.TagName {
		return &release, nil
	}

	return nil, nil // Up to date
}

// OpenUpdatePage opens the release page in browser.
func OpenUpdatePage(url string) error {
	return runner.OpenBrowser(url)
}
