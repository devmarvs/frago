package main

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/devmarvs/frago/internal/caddy"
	"github.com/devmarvs/frago/internal/port"
	"github.com/devmarvs/frago/internal/runner"
	"github.com/devmarvs/frago/internal/server"
	"github.com/devmarvs/frago/internal/updater"
)

const appName = "Frago"

var appVersion = "dev"

const beboVersion = "v0.1.0"
const fyneVersion = "v2.7.2"
const defaultVersionLabel = "Default (System Path)"
const appID = "com.devmarvs.frago"
const prefsStateKey = "project_state_v1"

func main() {
	// Initialize the Runner Manager
	mgr := runner.NewManager()

	// Initialize and Start Bebo Server
	apiPort, err := port.FindFreePort(5600, 5799)
	if err != nil {
		apiPort = 5678
	}
	go func() {
		srv := server.New(mgr, apiPort)
		fmt.Printf("Starting Bebo API on 127.0.0.1:%d\n", apiPort)
		if err := srv.Run(context.Background()); err != nil {
			fmt.Printf("Bebo API server error: %v\n", err)
		}
	}()

	a := app.NewWithID(appID)

	windowTitle := "Frago - FrankenPHP Launcher (Powered by Bebo)"
	if runtime.GOOS == "darwin" {
		windowTitle = "Frago · FrankenPHP Launcher"
	} else if runtime.GOOS == "windows" {
		windowTitle = "Frago – FrankenPHP Launcher"
	}

	w := a.NewWindow(windowTitle)
	w.Resize(fyne.NewSize(800, 600))

	// UI Elements
	pathEntry := widget.NewEntry()
	pathEntry.SetPlaceHolder("Select project directory...")

	// Version Selector
	versions, _ := runner.DetectVersions()
	var versionOptions []string
	versionMap := make(map[string]string)

	// Always add default "frankenphp" in path
	versionOptions = append(versionOptions, defaultVersionLabel)
	versionMap[defaultVersionLabel] = ""

	for _, v := range versions {
		label := v.Label
		// avoid duplicates if default is same as detected
		if _, exists := versionMap[label]; !exists {
			versionOptions = append(versionOptions, label)
			versionMap[label] = v.Path
		}
	}

	versionSelect := widget.NewSelect(versionOptions, nil)
	versionSelect.SetSelected(versionOptions[0])

	// Update Checker
	var updateBtn *widget.Button
	updateBtn = widget.NewButton("Check for Updates", func() {
		updateBtn.Disable()
		updateBtn.SetText("Checking...")

		go func() {
			release, err := updater.CheckForUpdates()

			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, w)
					updateBtn.SetText("Check for Updates")
					updateBtn.Enable()
					return
				}

				if release != nil {
					dialog.ShowConfirm("Update Available",
						fmt.Sprintf("New version %s is available.\n\n%s", release.TagName, release.Body),
						func(download bool) {
							if download {
								updater.OpenUpdatePage(release.HtmlUrl)
							}
						}, w)
				} else {
					dialog.ShowInformation("Up to Date", "You are using the latest version of FrankenPHP.", w)
				}
				updateBtn.SetText("Check for Updates")
				updateBtn.Enable()
			})
		}()
	})

	type projectInfo struct {
		Path             string
		LastPort         int
		LastURL          string
		LastVersionLabel string
		LastBinaryPath   string
		Pinned           bool
		AutoStart        bool
		LastUsed         time.Time
	}

	type storedProject struct {
		Path             string `json:"path"`
		LastPort         int    `json:"last_port,omitempty"`
		LastURL          string `json:"last_url,omitempty"`
		LastVersionLabel string `json:"last_version_label,omitempty"`
		LastBinaryPath   string `json:"last_binary_path,omitempty"`
		Pinned           bool   `json:"pinned,omitempty"`
		AutoStart        bool   `json:"auto_start,omitempty"`
		LastUsedUnix     int64  `json:"last_used_unix,omitempty"`
	}

	type storedState struct {
		Projects []storedProject `json:"projects"`
	}

	actionRow := func(buttons ...fyne.CanvasObject) *fyne.Container {
		objects := make([]fyne.CanvasObject, 0, len(buttons)+1)
		objects = append(objects, layout.NewSpacer())
		objects = append(objects, buttons...)
		return container.NewHBox(objects...)
	}

	formatUptime := func(start time.Time, running bool) string {
		if !running || start.IsZero() {
			return "stopped"
		}
		return time.Since(start).Round(time.Second).String()
	}

	appListContainer := container.NewVBox()
	recentListContainer := container.NewVBox()
	projects := make(map[string]*projectInfo)
	projectOrder := make([]string, 0)
	prefs := a.Preferences()

	ensureProject := func(path string) (*projectInfo, bool) {
		info, ok := projects[path]
		if ok {
			return info, false
		}
		info = &projectInfo{Path: path}
		projects[path] = info
		projectOrder = append(projectOrder, path)
		return info, true
	}

	sortedProjects := func() []*projectInfo {
		list := make([]*projectInfo, 0, len(projects))
		for _, info := range projects {
			list = append(list, info)
		}

		orderIndex := make(map[string]int, len(projectOrder))
		for i, path := range projectOrder {
			orderIndex[path] = i
		}

		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Pinned != list[j].Pinned {
				return list[i].Pinned
			}

			if list[i].LastUsed.IsZero() != list[j].LastUsed.IsZero() {
				return !list[i].LastUsed.IsZero()
			}

			if !list[i].LastUsed.Equal(list[j].LastUsed) {
				return list[i].LastUsed.After(list[j].LastUsed)
			}

			return orderIndex[list[i].Path] < orderIndex[list[j].Path]
		})

		return list
	}

	saveState := func() {
		state := storedState{
			Projects: make([]storedProject, 0, len(projectOrder)),
		}
		for _, path := range projectOrder {
			info := projects[path]
			if info == nil {
				continue
			}
			lastUsed := int64(0)
			if !info.LastUsed.IsZero() {
				lastUsed = info.LastUsed.Unix()
			}
			state.Projects = append(state.Projects, storedProject{
				Path:             info.Path,
				LastPort:         info.LastPort,
				LastURL:          info.LastURL,
				LastVersionLabel: info.LastVersionLabel,
				LastBinaryPath:   info.LastBinaryPath,
				Pinned:           info.Pinned,
				AutoStart:        info.AutoStart,
				LastUsedUnix:     lastUsed,
			})
		}
		raw, err := json.Marshal(state)
		if err != nil {
			fmt.Printf("Failed to save project state: %v\n", err)
			return
		}
		prefs.SetString(prefsStateKey, string(raw))
	}

	loadState := func() {
		raw := prefs.String(prefsStateKey)
		if raw == "" {
			return
		}
		var state storedState
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			fmt.Printf("Failed to load project state: %v\n", err)
			return
		}
		for _, stored := range state.Projects {
			if stored.Path == "" {
				continue
			}
			info, _ := ensureProject(stored.Path)
			info.LastPort = stored.LastPort
			info.LastURL = stored.LastURL
			info.LastVersionLabel = stored.LastVersionLabel
			info.LastBinaryPath = stored.LastBinaryPath
			info.Pinned = stored.Pinned
			info.AutoStart = stored.AutoStart
			if stored.LastUsedUnix > 0 {
				info.LastUsed = time.Unix(stored.LastUsedUnix, 0)
			}
		}
	}

	resolveStartOptions := func(info *projectInfo) (string, string) {
		binaryPath := info.LastBinaryPath
		versionLabel := info.LastVersionLabel
		if strings.HasPrefix(versionLabel, defaultVersionLabel) {
			versionLabel = ""
		}
		if binaryPath == "" {
			if mapped, ok := versionMap[versionLabel]; ok {
				binaryPath = mapped
			} else if versionLabel != "" {
				versionLabel = ""
			}
		}
		return binaryPath, versionLabel
	}

	startProject := func(info *projectInfo, binaryPath string, versionLabel string) error {
		caddyConfig, err := caddy.EnsureCaddyfile(info.Path, mgr.UsedPorts())
		if err != nil {
			return fmt.Errorf("caddyfile error: %w", err)
		}

		if err := mgr.Start(info.Path, caddyConfig, binaryPath, versionLabel); err != nil {
			return fmt.Errorf("start error: %w", err)
		}

		info.LastPort = caddyConfig.Port
		info.LastURL = fmt.Sprintf("http://localhost:%d", caddyConfig.Port)
		info.LastUsed = time.Now()
		info.LastVersionLabel = versionLabel
		info.LastBinaryPath = binaryPath
		return nil
	}
	var refreshAppList func()

	refreshAppList = func() {
		appListContainer.Objects = nil
		recentListContainer.Objects = nil

		processes := mgr.List()
		running := make(map[string]*runner.Process)
		stateDirty := false
		for _, p := range processes {
			running[p.ProjectPath] = p
			info, created := ensureProject(p.ProjectPath)
			if created {
				stateDirty = true
			}
			if info.LastPort != p.Port {
				info.LastPort = p.Port
				stateDirty = true
			}
			if info.LastURL != p.URL {
				info.LastURL = p.URL
				stateDirty = true
			}
			if info.LastVersionLabel != p.VersionLabel {
				info.LastVersionLabel = p.VersionLabel
				stateDirty = true
			}
			if info.LastBinaryPath != p.BinaryPath {
				info.LastBinaryPath = p.BinaryPath
				stateDirty = true
			}
			if p.StartedAt.After(info.LastUsed) {
				info.LastUsed = p.StartedAt
				stateDirty = true
			}
		}

		ordered := sortedProjects()
		if len(ordered) == 0 {
			appListContainer.Add(widget.NewLabel("No projects yet. Use New Project to launch one."))
			recentListContainer.Add(widget.NewLabel("No recent projects yet."))
		} else {
			for _, info := range ordered {
				infoCopy := info
				proc, isRunning := running[info.Path]

				lbl := widget.NewLabel(info.Path)
				lbl.Wrapping = fyne.TextWrapBreak

				versionLabel := info.LastVersionLabel
				url := info.LastURL
				startedAt := time.Time{}
				if isRunning {
					if proc.VersionLabel != "" {
						versionLabel = proc.VersionLabel
					}
					if proc.URL != "" {
						url = proc.URL
					}
					startedAt = proc.StartedAt
				}

				if versionLabel == "" {
					versionLabel = "Unknown"
				}
				if url == "" {
					url = "n/a"
				}

				statusLabel := widget.NewLabel(fmt.Sprintf("PHP: %s | URL: %s | Uptime: %s", versionLabel, url, formatUptime(startedAt, isRunning)))
				statusLabel.Wrapping = fyne.TextWrapBreak

				copyURL := url
				copyBtn := widget.NewButton("Copy URL", func() {
					w.Clipboard().SetContent(copyURL)
				})
				if copyURL == "n/a" {
					copyBtn.Disable()
				}

				statusRow := container.NewBorder(nil, nil, nil, copyBtn, statusLabel)

				pinLabel := "Pin"
				if info.Pinned {
					pinLabel = "Unpin"
				}
				pinBtn := widget.NewButton(pinLabel, func() {
					infoCopy.Pinned = !infoCopy.Pinned
					saveState()
					refreshAppList()
				})

				autoStartCheck := widget.NewCheck("Auto-start", nil)
				autoStartCheck.SetChecked(info.AutoStart)
				autoStartCheck.OnChanged = func(checked bool) {
					infoCopy.AutoStart = checked
					saveState()
					refreshAppList()
				}

				pathCopy := info.Path
				var primaryBtn *widget.Button
				var actionButtons []fyne.CanvasObject

				if isRunning {
					urlCopy := proc.URL
					stopBtn := widget.NewButton("Stop", nil)
					stopBtn.Importance = widget.DangerImportance

					primaryBtn = widget.NewButton("Open", func() {
						_ = runner.OpenBrowser(urlCopy)
						infoCopy.LastUsed = time.Now()
						saveState()
						refreshAppList()
					})

					stopBtn.OnTapped = func() {
						if err := mgr.Stop(pathCopy); err != nil {
							dialog.ShowError(err, w)
							return
						}
						refreshAppList()
					}

					actionButtons = []fyne.CanvasObject{autoStartCheck, primaryBtn, stopBtn, pinBtn}
				} else {
					deleteBtn := widget.NewButton("Delete", func() {
						delete(projects, pathCopy)
						for i, pth := range projectOrder {
							if pth == pathCopy {
								projectOrder = append(projectOrder[:i], projectOrder[i+1:]...)
								break
							}
						}
						saveState()
						refreshAppList()
					})
					deleteBtn.Importance = widget.DangerImportance

					primaryBtn = widget.NewButton("Run", func() {
						selectedLabel := versionSelect.Selected
						versionLabel := selectedLabel
						if strings.HasPrefix(selectedLabel, defaultVersionLabel) {
							versionLabel = ""
						}
						if err := startProject(infoCopy, versionMap[versionSelect.Selected], versionLabel); err != nil {
							dialog.ShowError(err, w)
							return
						}
						saveState()
						refreshAppList()
					})

					actionButtons = []fyne.CanvasObject{autoStartCheck, primaryBtn, deleteBtn, pinBtn}
				}

				appListContainer.Add(container.NewVBox(lbl, statusRow, actionRow(actionButtons...)))

				recentLabel := widget.NewLabel(info.Path)
				recentLabel.Wrapping = fyne.TextWrapBreak

				useBtn := widget.NewButton("Use", func() {
					pathEntry.SetText(infoCopy.Path)
					infoCopy.LastUsed = time.Now()
					saveState()
					refreshAppList()
				})

				recentPinLabel := "Pin"
				if info.Pinned {
					recentPinLabel = "Unpin"
				}
				recentPinBtn := widget.NewButton(recentPinLabel, func() {
					infoCopy.Pinned = !infoCopy.Pinned
					saveState()
					refreshAppList()
				})

				recentListContainer.Add(container.NewVBox(recentLabel, actionRow(useBtn, recentPinBtn)))
			}
		}
		appListContainer.Refresh()
		recentListContainer.Refresh()
		if stateDirty {
			saveState()
		}
	}

	loadState()

	// Initial refresh
	refreshAppList()

	autoStartProjects := func() {
		var errs []string
		started := 0
		for _, path := range projectOrder {
			info := projects[path]
			if info == nil || !info.AutoStart {
				continue
			}
			if _, exists := mgr.Get(info.Path); exists {
				continue
			}

			binaryPath, versionLabel := resolveStartOptions(info)
			if err := startProject(info, binaryPath, versionLabel); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", info.Path, err))
				continue
			}
			started++
		}
		if started > 0 {
			saveState()
		}
		refreshAppList()
		if len(errs) > 0 {
			dialog.ShowError(fmt.Errorf("Auto-start failures:\n%s", strings.Join(errs, "\n")), w)
		}
	}

	// Choose Folder Action
	chooseBtn := widget.NewButton("Choose Folder", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			pathEntry.SetText(uri.Path())
		}, w)
	})

	runBtn := widget.NewButton("Run FrankenPHP", func() {
		dir := pathEntry.Text
		if dir == "" {
			dialog.ShowError(fmt.Errorf("please select a directory"), w)
			return
		}

		// Check if already running
		if _, exists := mgr.Get(dir); exists {
			dialog.ShowInformation("Already Running", "This project is already running.", w)
			return
		}

		info, _ := ensureProject(dir)
		selectedLabel := versionSelect.Selected
		versionLabel := selectedLabel
		if strings.HasPrefix(selectedLabel, defaultVersionLabel) {
			versionLabel = ""
		}
		if err := startProject(info, versionMap[versionSelect.Selected], versionLabel); err != nil {
			dialog.ShowError(err, w)
			return
		}

		// Clear entry and refresh list
		pathEntry.SetText("")
		saveState()
		refreshAppList()
	})
	runBtn.Importance = widget.HighImportance

	// Poller to keep UI in sync
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			fyne.Do(func() {
				refreshAppList()
			})
		}
	}()

	// Manual refresh button
	refreshBtn := widget.NewButton("Refresh List", func() {
		refreshAppList()
	})

	startAllBtn := widget.NewButton("Start All", func() {
		var errs []string
		started := 0
		for _, path := range projectOrder {
			info := projects[path]
			if info == nil {
				continue
			}
			if _, exists := mgr.Get(info.Path); exists {
				continue
			}

			binaryPath, versionLabel := resolveStartOptions(info)
			if err := startProject(info, binaryPath, versionLabel); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", info.Path, err))
				continue
			}
			started++
		}
		if started > 0 {
			saveState()
		}
		refreshAppList()
		if len(errs) > 0 {
			dialog.ShowError(fmt.Errorf("Some projects failed to start:\n%s", strings.Join(errs, "\n")), w)
		}
	})

	apiLabel := widget.NewLabel(fmt.Sprintf("API available at http://localhost:%d", apiPort))
	apiLabel.TextStyle = fyne.TextStyle{Monospace: true}
	apiLabel.Alignment = fyne.TextAlignCenter

	title := widget.NewLabelWithStyle("Frago FrankenPHP Launcher", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	subtitle := widget.NewLabel("Launch and manage FrankenPHP projects")
	header := container.NewVBox(title, subtitle)

	projectDirField := container.NewVBox(
		pathEntry,
		actionRow(chooseBtn),
	)
	versionField := container.NewVBox(
		versionSelect,
		actionRow(updateBtn),
	)
	launchForm := widget.NewForm(
		&widget.FormItem{Text: "Project Directory", Widget: projectDirField},
		&widget.FormItem{Text: "PHP Version", Widget: versionField},
	)

	recentHeader := widget.NewLabelWithStyle("Recent Projects", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	recentScroll := container.NewScroll(recentListContainer)
	recentScroll.SetMinSize(fyne.NewSize(0, 140))

	launchCard := widget.NewCard("New Project", "Configure and launch FrankenPHP for your PHP application.", container.NewVBox(
		launchForm,
		runBtn,
		widget.NewSeparator(),
		recentHeader,
		recentScroll,
	))

	listHeader := container.NewBorder(nil, nil, nil, container.NewHBox(startAllBtn, refreshBtn), nil)
	scrollList := container.NewScroll(appListContainer)
	scrollList.SetMinSize(fyne.NewSize(0, 300))

	listArea := container.NewBorder(listHeader, nil, nil, nil, scrollList)

	runningCard := widget.NewCard("Running Applications", "", listArea)

	body := container.NewGridWithColumns(2,
		launchCard,
		runningCard,
	)

	content := container.NewBorder(
		header,
		container.NewVBox(widget.NewSeparator(), apiLabel),
		nil, nil,
		container.NewPadded(body),
	)

	w.SetContent(content)
	go func() {
		time.Sleep(200 * time.Millisecond)
		fyne.Do(func() {
			autoStartProjects()
		})
	}()

	aboutItem := fyne.NewMenuItem("About Frago", func() {
		binaryPath := runner.DefaultFrankenPHPBinary()
		frankenVer, err := runner.GetFrankenPHPVersion(binaryPath)
		if err != nil || frankenVer == "" {
			frankenVer = "Unknown"
		}

		text := fmt.Sprintf("%s version: %s\nFrankenPHP: %s\nBebo: %s\nFyne: %s\n\nPowered by Bebo and Fyne.", appName, appVersion, frankenVer, beboVersion, fyneVersion)
		dialog.ShowInformation("About Frago", text, w)
	})

	mainMenu := fyne.NewMainMenu(
		fyne.NewMenu("Frago", aboutItem),
	)
	w.SetMainMenu(mainMenu)
	w.ShowAndRun()
}
