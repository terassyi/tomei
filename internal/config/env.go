package config

import (
	"os"
	"runtime"
	"strings"
)

// OS represents the operating system.
type OS string

const (
	OSLinux  OS = "linux"
	OSDarwin OS = "darwin"
)

// Arch represents the CPU architecture.
type Arch string

const (
	ArchAMD64 Arch = "amd64"
	ArchARM64 Arch = "arm64"
)

// Env represents environment variables injected into CUE configuration.
type Env struct {
	OS       OS   `json:"os"`
	Arch     Arch `json:"arch"`
	Headless bool `json:"headless"`
}

// DetectEnv detects the current environment.
func DetectEnv() *Env {
	return &Env{
		OS:       detectOS(),
		Arch:     detectArch(),
		Headless: detectHeadless(),
	}
}

func detectOS() OS {
	switch runtime.GOOS {
	case "darwin":
		return OSDarwin
	default:
		return OSLinux
	}
}

func detectArch() Arch {
	switch runtime.GOARCH {
	case "arm64":
		return ArchARM64
	default:
		return ArchAMD64
	}
}

func detectHeadless() bool {
	// Check for container environment
	if isContainer() {
		return true
	}

	// Check for DISPLAY on Linux
	if runtime.GOOS == "linux" {
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return true
		}
	}

	// Check for SSH session
	if os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != "" {
		return true
	}

	// Check for CI environment
	if os.Getenv("CI") != "" {
		return true
	}

	return false
}

func isContainer() bool {
	// Check for Docker
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	// Check for container environment variable
	if os.Getenv("container") != "" {
		return true
	}

	// Check for Kubernetes
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}

	// Check cgroup for docker/lxc/containerd
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		content := string(data)
		if strings.Contains(content, "docker") ||
			strings.Contains(content, "lxc") ||
			strings.Contains(content, "kubepods") ||
			strings.Contains(content, "containerd") {
			return true
		}
	}

	return false
}
