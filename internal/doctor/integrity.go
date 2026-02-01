package doctor

import (
	"os"
	"path/filepath"

	"github.com/terassyi/toto/internal/path"
)

// checkStateIntegrity verifies that the state matches the filesystem.
func (d *Doctor) checkStateIntegrity() ([]StateIssue, error) {
	var issues []StateIssue

	// Check tools
	toolIssues, err := d.checkToolIntegrity()
	if err != nil {
		return nil, err
	}
	issues = append(issues, toolIssues...)

	// Check runtimes
	runtimeIssues, err := d.checkRuntimeIntegrity()
	if err != nil {
		return nil, err
	}
	issues = append(issues, runtimeIssues...)

	return issues, nil
}

// checkToolIntegrity checks that all tools in state have valid files.
func (d *Doctor) checkToolIntegrity() ([]StateIssue, error) {
	if d.state == nil || d.state.Tools == nil {
		return nil, nil
	}

	var issues []StateIssue

	for name, tool := range d.state.Tools {
		// Check if the binary/symlink exists
		if tool.BinPath != "" {
			binPath, err := path.Expand(tool.BinPath)
			if err != nil {
				return nil, err
			}

			info, err := os.Lstat(binPath)
			if err != nil {
				if os.IsNotExist(err) {
					issues = append(issues, StateIssue{
						Kind: StateIssueMissingBinary,
						Name: name,
						Path: binPath,
					})
					continue
				}
				return nil, err
			}

			// Check if it's a symlink and if it's broken
			if info.Mode()&os.ModeSymlink != 0 {
				target, err := os.Readlink(binPath)
				if err != nil {
					issues = append(issues, StateIssue{
						Kind: StateIssueBrokenSymlink,
						Name: name,
						Path: binPath,
					})
					continue
				}

				// Check if target exists
				targetPath := target
				if !filepath.IsAbs(target) {
					targetPath = filepath.Join(filepath.Dir(binPath), target)
				}

				if _, err := os.Stat(targetPath); os.IsNotExist(err) {
					issues = append(issues, StateIssue{
						Kind:   StateIssueBrokenSymlink,
						Name:   name,
						Path:   binPath,
						Target: target,
					})
				}
			}
		}

		// Check if the install directory exists (for download pattern tools)
		if tool.InstallPath != "" {
			installPath, err := path.Expand(tool.InstallPath)
			if err != nil {
				return nil, err
			}

			if _, err := os.Stat(installPath); os.IsNotExist(err) {
				issues = append(issues, StateIssue{
					Kind: StateIssueMissingInstallDir,
					Name: name,
					Path: installPath,
				})
			}
		}
	}

	return issues, nil
}

// checkRuntimeIntegrity checks that all runtimes in state have valid files.
func (d *Doctor) checkRuntimeIntegrity() ([]StateIssue, error) {
	if d.state == nil || d.state.Runtimes == nil {
		return nil, nil
	}

	var issues []StateIssue

	for name, runtime := range d.state.Runtimes {
		// Check if the install directory exists
		if runtime.InstallPath != "" {
			installPath, err := path.Expand(runtime.InstallPath)
			if err != nil {
				return nil, err
			}

			if _, err := os.Stat(installPath); os.IsNotExist(err) {
				issues = append(issues, StateIssue{
					Kind: StateIssueMissingInstallDir,
					Name: name,
					Path: installPath,
				})
			}
		}

		// Check if binaries exist in BinDir (symlinks to runtime binaries)
		// Skip if BinDir is empty (no symlinks were created)
		if runtime.BinDir == "" {
			continue
		}
		for _, binary := range runtime.Binaries {
			binPath := filepath.Join(runtime.BinDir, binary)

			info, err := os.Lstat(binPath)
			if err != nil {
				if os.IsNotExist(err) {
					issues = append(issues, StateIssue{
						Kind: StateIssueMissingBinary,
						Name: name,
						Path: binPath,
					})
					continue
				}
				return nil, err
			}

			// Check if it's a symlink and if it's broken
			if info.Mode()&os.ModeSymlink != 0 {
				target, err := os.Readlink(binPath)
				if err != nil {
					issues = append(issues, StateIssue{
						Kind: StateIssueBrokenSymlink,
						Name: name,
						Path: binPath,
					})
					continue
				}

				targetPath := target
				if !filepath.IsAbs(target) {
					targetPath = filepath.Join(filepath.Dir(binPath), target)
				}

				if _, err := os.Stat(targetPath); os.IsNotExist(err) {
					issues = append(issues, StateIssue{
						Kind:   StateIssueBrokenSymlink,
						Name:   name,
						Path:   binPath,
						Target: target,
					})
				}
			}
		}
	}

	return issues, nil
}
