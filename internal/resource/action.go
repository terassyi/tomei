package resource

// ActionType represents the operation to perform on a resource.
type ActionType string

const (
	ActionNone      ActionType = "none"      // No change needed
	ActionInstall   ActionType = "install"   // New installation
	ActionUpgrade   ActionType = "upgrade"   // Version upgrade
	ActionDowngrade ActionType = "downgrade" // Version downgrade
	ActionReinstall ActionType = "reinstall" // Reinstall due to taint
	ActionRemove    ActionType = "remove"    // Remove (spec deleted)
)

// Action represents a planned operation during the diff phase.
type Action struct {
	Resource   *Resource
	ActionType ActionType
	Reason     string // Human-readable reason for the action
}

// NeedsExecution returns true if this action requires work.
func (a *Action) NeedsExecution() bool {
	return a.ActionType != ActionNone
}
