package main

import (
	"context"
	"fmt"
	"runtime"
	"sort"
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

	a := app.New()

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
	currentVersionLabel := func() string {
		if versionSelect.Selected == defaultVersionLabel {
			return ""
		}
		return versionSelect.Selected
	}

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
		Pinned           bool
		LastUsed         time.Time
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

	ensureProject := func(path string) *projectInfo {
		info, ok := projects[path]
		if ok {
			return info
		}
		info = &projectInfo{Path: path}
		projects[path] = info
		projectOrder = append(projectOrder, path)
		return info
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
	var refreshAppList func()

	refreshAppList = func() {
		appListContainer.Objects = nil
		recentListContainer.Objects = nil

		processes := mgr.List()
		running := make(map[string]*runner.Process)
		for _, p := range processes {
			running[p.ProjectPath] = p
			info := ensureProject(p.ProjectPath)
			info.LastPort = p.Port
			info.LastURL = p.URL
			info.LastVersionLabel = p.VersionLabel
			if p.StartedAt.After(info.LastUsed) {
				info.LastUsed = p.StartedAt
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
					refreshAppList()
				})

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
						refreshAppList()
					})

					stopBtn.OnTapped = func() {
						if err := mgr.Stop(pathCopy); err != nil {
							dialog.ShowError(err, w)
							return
						}
						refreshAppList()
					}

					actionButtons = []fyne.CanvasObject{primaryBtn, stopBtn, pinBtn}
				} else {
					deleteBtn := widget.NewButton("Delete", func() {
						delete(projects, pathCopy)
						for i, pth := range projectOrder {
							if pth == pathCopy {
								projectOrder = append(projectOrder[:i], projectOrder[i+1:]...)
								break
							}
						}
						refreshAppList()
					})
					deleteBtn.Importance = widget.DangerImportance

					primaryBtn = widget.NewButton("Run", func() {
						caddyConfig, err := caddy.EnsureCaddyfile(pathCopy, mgr.UsedPorts())
						if err != nil {
							dialog.ShowError(fmt.Errorf("caddyfile error: %w", err), w)
							return
						}

						selectedLabel := currentVersionLabel()
						if err := mgr.Start(pathCopy, caddyConfig, versionMap[versionSelect.Selected], selectedLabel); err != nil {
							dialog.ShowError(fmt.Errorf("start error: %w", err), w)
							return
						}

						infoCopy.LastUsed = time.Now()
						if selectedLabel != "" {
							infoCopy.LastVersionLabel = selectedLabel
						}
						refreshAppList()
					})

					actionButtons = []fyne.CanvasObject{primaryBtn, deleteBtn, pinBtn}
				}

				appListContainer.Add(container.NewVBox(lbl, statusRow, actionRow(actionButtons...)))

				recentLabel := widget.NewLabel(info.Path)
				recentLabel.Wrapping = fyne.TextWrapBreak

				useBtn := widget.NewButton("Use", func() {
					pathEntry.SetText(infoCopy.Path)
					infoCopy.LastUsed = time.Now()
					refreshAppList()
				})

				recentPinLabel := "Pin"
				if info.Pinned {
					recentPinLabel = "Unpin"
				}
				recentPinBtn := widget.NewButton(recentPinLabel, func() {
					infoCopy.Pinned = !infoCopy.Pinned
					refreshAppList()
				})

				recentListContainer.Add(container.NewVBox(recentLabel, actionRow(useBtn, recentPinBtn)))
			}
		}
		appListContainer.Refresh()
		recentListContainer.Refresh()
	}

	// Initial refresh
	refreshAppList()

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

		caddyConfig, err := caddy.EnsureCaddyfile(dir, mgr.UsedPorts())
		if err != nil {
			dialog.ShowError(fmt.Errorf("caddyfile error: %w", err), w)
			return
		}

		selectedLabel := currentVersionLabel()
		if err := mgr.Start(dir, caddyConfig, versionMap[versionSelect.Selected], selectedLabel); err != nil {
			dialog.ShowError(fmt.Errorf("start error: %w", err), w)
			return
		}

		info := ensureProject(dir)
		info.LastUsed = time.Now()
		if selectedLabel != "" {
			info.LastVersionLabel = selectedLabel
		}

		// Clear entry and refresh list
		pathEntry.SetText("")
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

	listHeader := container.NewBorder(nil, nil, nil, refreshBtn, nil)
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
