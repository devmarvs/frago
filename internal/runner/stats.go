package runner

import (
	"encoding/csv"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

type ProcessStats struct {
	CPUPercent float64
	RSSBytes   int64
}

// GetProcessStats returns CPU percent and RSS memory for a running process.
// Best effort: on Windows, RAM may be available even if CPU percent is not.
func GetProcessStats(pid int) (ProcessStats, error) {
	if pid <= 0 {
		return ProcessStats{}, fmt.Errorf("invalid pid: %d", pid)
	}
	if runtime.GOOS == "windows" {
		return getWindowsProcessStats(pid)
	}
	return getUnixProcessStats(pid)
}

func getUnixProcessStats(pid int) (ProcessStats, error) {
	cmd := exec.Command("ps", "-o", "%cpu=", "-o", "rss=", "-p", strconv.Itoa(pid))
	out, err := cmd.Output()
	if err != nil {
		return ProcessStats{}, err
	}

	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return ProcessStats{}, fmt.Errorf("unexpected ps output: %q", strings.TrimSpace(string(out)))
	}

	cpuStr := strings.ReplaceAll(fields[0], ",", ".")
	cpu, err := strconv.ParseFloat(cpuStr, 64)
	if err != nil {
		return ProcessStats{}, fmt.Errorf("parse cpu: %w", err)
	}

	rssKB, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return ProcessStats{}, fmt.Errorf("parse rss: %w", err)
	}

	return ProcessStats{
		CPUPercent: cpu,
		RSSBytes:   rssKB * 1024,
	}, nil
}

func getWindowsProcessStats(pid int) (ProcessStats, error) {
	stats, err := getWindowsStatsWMIC(pid)
	if err == nil {
		return stats, nil
	}

	rssBytes, rssErr := getWindowsRSSFromTasklist(pid)
	if rssErr != nil {
		return ProcessStats{}, err
	}

	return ProcessStats{
		CPUPercent: -1,
		RSSBytes:   rssBytes,
	}, nil
}

func getWindowsStatsWMIC(pid int) (ProcessStats, error) {
	cmd := exec.Command(
		"wmic",
		"path",
		"Win32_PerfFormattedData_PerfProc_Process",
		"where",
		fmt.Sprintf("IDProcess=%d", pid),
		"get",
		"PercentProcessorTime,WorkingSet",
		"/format:csv",
	)
	out, err := cmd.Output()
	if err != nil {
		return ProcessStats{}, err
	}

	cpu, rss, err := parseWMICStats(string(out))
	if err != nil {
		return ProcessStats{}, err
	}

	return ProcessStats{
		CPUPercent: cpu,
		RSSBytes:   rss,
	}, nil
}

func parseWMICStats(output string) (float64, int64, error) {
	reader := csv.NewReader(strings.NewReader(output))
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return 0, 0, err
	}

	header := []string(nil)
	cpuIdx := -1
	rssIdx := -1

	for _, record := range records {
		if len(record) == 0 {
			continue
		}

		if header == nil {
			for i, field := range record {
				if field == "PercentProcessorTime" {
					cpuIdx = i
				}
				if field == "WorkingSet" {
					rssIdx = i
				}
			}
			if cpuIdx != -1 && rssIdx != -1 {
				header = record
			}
			continue
		}

		if len(record) <= cpuIdx || len(record) <= rssIdx {
			continue
		}

		cpu, err := strconv.ParseFloat(strings.TrimSpace(record[cpuIdx]), 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse cpu: %w", err)
		}
		rss, err := strconv.ParseInt(strings.TrimSpace(record[rssIdx]), 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse rss: %w", err)
		}
		return cpu, rss, nil
	}

	if header != nil {
		return 0, 0, fmt.Errorf("no data rows in wmic output")
	}
	return 0, 0, fmt.Errorf("missing wmic headers")
}

func getWindowsRSSFromTasklist(pid int) (int64, error) {
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH")
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	output := strings.TrimSpace(string(out))
	if output == "" || strings.HasPrefix(output, "INFO:") {
		return 0, fmt.Errorf("tasklist returned no rows")
	}

	reader := csv.NewReader(strings.NewReader(output))
	records, err := reader.ReadAll()
	if err != nil {
		return 0, err
	}
	if len(records) == 0 || len(records[0]) < 5 {
		return 0, fmt.Errorf("unexpected tasklist output")
	}

	memField := strings.TrimSpace(records[0][4])
	memField = strings.TrimSuffix(memField, " K")
	memField = strings.TrimSuffix(memField, " KB")
	memField = strings.ReplaceAll(memField, ",", "")

	kb, err := strconv.ParseInt(memField, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse memory: %w", err)
	}

	return kb * 1024, nil
}
