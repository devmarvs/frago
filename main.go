package main

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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
	versionOptions = append(versionOptions, "Default (System Path)")
	versionMap["Default (System Path)"] = ""

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
		Path     string
		LastPort int
		LastURL  string
	}

	appListContainer := container.NewVBox()
	projects := make(map[string]*projectInfo)
	projectOrder := make([]string, 0)
	var refreshAppList func()

	refreshAppList = func() {
		appListContainer.Objects = nil

		processes := mgr.List()
		running := make(map[string]*runner.Process)
		for _, p := range processes {
			running[p.ProjectPath] = p
			info, ok := projects[p.ProjectPath]
			if !ok {
				info = &projectInfo{Path: p.ProjectPath}
				projects[p.ProjectPath] = info
				projectOrder = append(projectOrder, p.ProjectPath)
			}
			info.LastPort = p.Port
			info.LastURL = p.URL
		}

		if len(projectOrder) == 0 {
			appListContainer.Add(widget.NewLabel("No projects yet. Use New Project to launch one."))
		} else {
			for _, path := range projectOrder {
				info := projects[path]
				proc, isRunning := running[path]

				labelText := info.Path
				if isRunning {
					labelText = fmt.Sprintf("%s (Port: %d)", info.Path, proc.Port)
				} else if info.LastPort != 0 {
					labelText = fmt.Sprintf("%s (Stopped, last port: %d)", info.Path, info.LastPort)
				} else {
					labelText = fmt.Sprintf("%s (Stopped)", info.Path)
				}

				lbl := widget.NewLabel(labelText)

				pathCopy := info.Path
				var primaryBtn *widget.Button

				if isRunning {
					urlCopy := proc.URL
					stopBtn := widget.NewButton("Stop", nil)
					stopBtn.Importance = widget.DangerImportance

					primaryBtn = widget.NewButton("Open", func() {
						runner.OpenBrowser(urlCopy)
					})

					stopBtn.OnTapped = func() {
						if err := mgr.Stop(pathCopy); err != nil {
							dialog.ShowError(err, w)
							return
						}
						refreshAppList()
					}

					appListContainer.Add(container.NewBorder(nil, nil, nil, container.NewHBox(primaryBtn, stopBtn), lbl))
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

						if err := mgr.Start(pathCopy, caddyConfig, versionMap[versionSelect.Selected]); err != nil {
							dialog.ShowError(fmt.Errorf("start error: %w", err), w)
							return
						}

						refreshAppList()
					})

					appListContainer.Add(container.NewBorder(nil, nil, nil, container.NewHBox(primaryBtn, deleteBtn), lbl))
				}
			}
		}
		appListContainer.Refresh()
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

		if err := mgr.Start(dir, caddyConfig, versionMap[versionSelect.Selected]); err != nil {
			dialog.ShowError(fmt.Errorf("start error: %w", err), w)
			return
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

	launchForm := widget.NewForm(
		&widget.FormItem{Text: "Project Directory", Widget: container.NewBorder(nil, nil, nil, chooseBtn, pathEntry)},
		&widget.FormItem{Text: "PHP Version", Widget: container.NewBorder(nil, nil, nil, updateBtn, versionSelect)},
	)

	launchCard := widget.NewCard("New Project", "Configure and launch FrankenPHP for your PHP application.", container.NewVBox(
		launchForm,
		runBtn,
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
