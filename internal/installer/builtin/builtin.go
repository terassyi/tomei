package builtin

import (
	"github.com/terassyi/toto/internal/installer/download"
	"github.com/terassyi/toto/internal/resource"
)

// installers holds all builtin installer definitions.
var installers = []*resource.Installer{
	download.BuiltinInstaller,
}

// installerMap provides quick lookup by name.
var installerMap = func() map[string]*resource.Installer {
	m := make(map[string]*resource.Installer, len(installers))
	for _, inst := range installers {
		m[inst.Metadata.Name] = inst
	}
	return m
}()

// Installers returns all builtin installer definitions.
// These are automatically available without user definition.
func Installers() []*resource.Installer {
	result := make([]*resource.Installer, len(installers))
	copy(result, installers)
	return result
}

// Get returns a builtin installer by name.
// Returns nil if not found.
func Get(name string) *resource.Installer {
	return installerMap[name]
}

// IsBuiltin returns true if the given name is a builtin installer.
func IsBuiltin(name string) bool {
	_, ok := installerMap[name]
	return ok
}
