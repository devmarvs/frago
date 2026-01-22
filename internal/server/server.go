package server

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/devmarvs/bebo"
	"github.com/devmarvs/bebo/middleware"
	"github.com/devmarvs/frago/internal/caddy"
	"github.com/devmarvs/frago/internal/runner"
)

type RunRequest struct {
	ProjectPath string `json:"project_path"`
	BinaryPath  string `json:"binary_path,omitempty"`
	Port        int    `json:"port,omitempty"`
}

type RunResponse struct {
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
	Port   int    `json:"port,omitempty"`
}

func New(mgr *runner.Manager, port int) *bebo.App {
	cfg := bebo.DefaultConfig()
	cfg.Address = fmt.Sprintf("127.0.0.1:%d", port)
	app := bebo.New(bebo.WithConfig(cfg))

	allowedBinaries := buildAllowedBinaries()

	// Middleware
	app.Use(middleware.RequestID(), middleware.Recover(), middleware.Logger())

	// Health check
	app.GET("/health", func(ctx *bebo.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Status endpoint
	app.GET("/api/status", func(ctx *bebo.Context) error {
		processes := mgr.List()
		var active []map[string]interface{}

		for _, p := range processes {
			active = append(active, map[string]interface{}{
				"project_path": p.ProjectPath,
				"url":          p.URL,
				"port":         p.Port,
			})
		}

		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"processes": active,
			"count":     len(active),
		})
	})

	// Run endpoint
	app.POST("/api/run", func(ctx *bebo.Context) error {
		var req RunRequest
		if err := ctx.BindJSON(&req); err != nil {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		}

		if req.ProjectPath == "" {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "project_path is required"})
		}
		if req.Port != 0 && (req.Port < 1 || req.Port > 65535) {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "port must be between 1 and 65535"})
		}

		// Check directory
		if _, err := os.Stat(req.ProjectPath); os.IsNotExist(err) {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Directory does not exist"})
		} else if err != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to access directory"})
		}

		if req.BinaryPath != "" {
			resolved, err := resolveBinaryPath(req.BinaryPath)
			if err != nil {
				return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "binary_path not found or not executable"})
			}
			if _, ok := allowedBinaries[resolved]; !ok {
				return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "binary_path is not a known FrankenPHP binary"})
			}
			if _, err := runner.GetFrankenPHPVersion(resolved); err != nil {
				return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "binary_path does not appear to be FrankenPHP"})
			}
			req.BinaryPath = resolved
		}

		// Ensure Caddyfile, avoiding ports already used by managed processes
		config, err := caddy.EnsureCaddyfile(req.ProjectPath, mgr.UsedPorts(), req.Port)
		if err != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Caddyfile error: %v", err)})
		}

		// Start Process
		if err := mgr.Start(req.ProjectPath, config, req.BinaryPath, ""); err != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to start: %v", err)})
		}

		return ctx.JSON(http.StatusOK, RunResponse{
			Status: "running",
			URL:    fmt.Sprintf("http://localhost:%d", config.Port),
			Port:   config.Port,
		})
	})

	// Stop endpoint
	app.POST("/api/stop", func(ctx *bebo.Context) error {
		var req RunRequest
		if err := ctx.BindJSON(&req); err != nil {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		}

		if req.ProjectPath == "" {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "project_path is required"})
		}

		if err := mgr.Stop(req.ProjectPath); err != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return ctx.JSON(http.StatusOK, map[string]string{"status": "stopped", "project_path": req.ProjectPath})
	})

	return app
}

func buildAllowedBinaries() map[string]struct{} {
	allowed := make(map[string]struct{})

	versions, err := runner.DetectVersions()
	if err == nil {
		for _, v := range versions {
			if resolved, err := resolveBinaryPath(v.Path); err == nil {
				allowed[resolved] = struct{}{}
			}
		}
	}

	if def := runner.DefaultFrankenPHPBinary(); def != "" {
		if resolved, err := resolveBinaryPath(def); err == nil {
			allowed[resolved] = struct{}{}
		}
	}

	return allowed
}

func resolveBinaryPath(path string) (string, error) {
	resolved := path
	if !filepath.IsAbs(path) {
		lookedUp, err := exec.LookPath(path)
		if err != nil {
			return "", err
		}
		resolved = lookedUp
	}

	if realPath, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = realPath
	}

	if st, err := os.Stat(resolved); err != nil || st.IsDir() {
		if err == nil {
			err = fmt.Errorf("binary path is a directory")
		}
		return "", err
	}

	return resolved, nil
}
