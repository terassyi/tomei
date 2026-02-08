package graph

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/fatih/color"

	"github.com/terassyi/tomei/internal/resource"
)

// Action represents the action to be taken on a resource.
type Action string

const (
	ActionInstall   Action = "install"
	ActionUpgrade   Action = "upgrade"
	ActionReinstall Action = "reinstall"
	ActionRemove    Action = "remove"
	ActionNone      Action = "none"
)

// ResourceInfo holds information about a resource for display.
type ResourceInfo struct {
	Kind    resource.Kind
	Name    string
	Version string
	Action  Action
}

// TreePrinter prints dependency graphs as ASCII trees with colors.
type TreePrinter struct {
	noColor bool
	writer  io.Writer

	// Color definitions
	installColor   *color.Color
	upgradeColor   *color.Color
	reinstallColor *color.Color
	removeColor    *color.Color
	noChangeColor  *color.Color
	kindColors     map[resource.Kind]*color.Color
}

// NewTreePrinter creates a new TreePrinter.
func NewTreePrinter(w io.Writer, noColor bool) *TreePrinter {
	if noColor {
		color.NoColor = true
	}

	return &TreePrinter{
		noColor:        noColor,
		writer:         w,
		installColor:   color.New(color.FgGreen),
		upgradeColor:   color.New(color.FgYellow),
		reinstallColor: color.New(color.FgCyan),
		removeColor:    color.New(color.FgRed),
		noChangeColor:  color.New(color.FgWhite),
		kindColors: map[resource.Kind]*color.Color{
			resource.KindRuntime:             color.New(color.FgBlue),
			resource.KindInstaller:           color.New(color.FgYellow),
			resource.KindInstallerRepository: color.New(color.FgCyan),
			resource.KindTool:                color.New(color.FgGreen),
		},
	}
}

// PrintTree prints the dependency graph as an ASCII tree.
func (p *TreePrinter) PrintTree(resolver Resolver, resourceInfo map[NodeID]ResourceInfo) {
	edges := resolver.GetEdges()
	nodes := resolver.GetNodes()

	// Build adjacency list: node -> children (dependents)
	// We want to show: parent depends on children, so we reverse the edge direction
	children := make(map[NodeID][]NodeID)
	hasParent := make(map[NodeID]bool)

	for _, edge := range edges {
		// edge.From depends on edge.To
		// In tree view: edge.To is parent, edge.From is child
		children[edge.To] = append(children[edge.To], edge.From)
		hasParent[edge.From] = true
	}

	// Find root nodes (no dependencies)
	var roots []NodeID
	for _, node := range nodes {
		if !hasParent[node.ID] {
			roots = append(roots, node.ID)
		}
	}

	// Sort roots for deterministic output
	slices.SortFunc(roots, func(a, b NodeID) int {
		return strings.Compare(string(a), string(b))
	})

	// Print each root tree
	for _, root := range roots {
		p.printNode(root, "", true, children, resourceInfo, true)
	}
}

func (p *TreePrinter) printNode(nodeID NodeID, prefix string, isLast bool, children map[NodeID][]NodeID, resourceInfo map[NodeID]ResourceInfo, isRoot bool) {
	// Determine connector
	var connector string
	if isRoot {
		connector = ""
	} else if isLast {
		connector = "└── "
	} else {
		connector = "├── "
	}

	// Get resource info
	info, hasInfo := resourceInfo[nodeID]

	// Format the node line
	line := p.formatNode(nodeID, info, hasInfo)

	fmt.Fprintf(p.writer, "%s%s%s\n", prefix, connector, line)

	// Get children and sort them
	nodeChildren := children[nodeID]
	slices.SortFunc(nodeChildren, func(a, b NodeID) int {
		return strings.Compare(string(a), string(b))
	})

	// Calculate new prefix for children
	var newPrefix string
	if isRoot {
		newPrefix = ""
	} else if isLast {
		newPrefix = prefix + "    "
	} else {
		newPrefix = prefix + "│   "
	}

	// Print children
	for i, child := range nodeChildren {
		isLastChild := i == len(nodeChildren)-1
		p.printNode(child, newPrefix, isLastChild, children, resourceInfo, false)
	}
}

func (p *TreePrinter) formatNode(nodeID NodeID, info ResourceInfo, hasInfo bool) string {
	var sb strings.Builder

	// Node name (Kind/Name)
	nodeName := string(nodeID)

	if hasInfo {
		// Version
		versionStr := ""
		if info.Version != "" {
			versionStr = fmt.Sprintf(" (%s)", info.Version)
		}

		// Action
		actionStr := ""
		var actionColor *color.Color
		switch info.Action {
		case ActionInstall:
			actionStr = " [+ install]"
			actionColor = p.installColor
		case ActionUpgrade:
			actionStr = " [~ upgrade]"
			actionColor = p.upgradeColor
		case ActionReinstall:
			actionStr = " [↻ reinstall]"
			actionColor = p.reinstallColor
		case ActionRemove:
			actionStr = " [- remove]"
			actionColor = p.removeColor
		case ActionNone:
			actionColor = p.noChangeColor
		}

		if actionColor != nil && info.Action != ActionNone {
			sb.WriteString(actionColor.Sprint(nodeName + versionStr + actionStr))
		} else {
			sb.WriteString(nodeName + versionStr)
		}
	} else {
		sb.WriteString(nodeName)
	}

	return sb.String()
}

// PrintLayers prints the execution layers.
func (p *TreePrinter) PrintLayers(layers []Layer, resourceInfo map[NodeID]ResourceInfo) {
	fmt.Fprintln(p.writer, "\nExecution Order:")
	for i, layer := range layers {
		var nodeNames []string
		for _, node := range layer.Nodes {
			nodeNames = append(nodeNames, fmt.Sprintf("%s/%s", node.Kind, node.Name))
		}
		fmt.Fprintf(p.writer, "  Layer %d: %s\n", i+1, strings.Join(nodeNames, ", "))
	}
}

// PrintSummary prints the action summary.
func (p *TreePrinter) PrintSummary(resourceInfo map[NodeID]ResourceInfo) {
	counts := map[Action]int{
		ActionInstall:   0,
		ActionUpgrade:   0,
		ActionReinstall: 0,
		ActionRemove:    0,
	}

	for _, info := range resourceInfo {
		if info.Action != ActionNone {
			counts[info.Action]++
		}
	}

	fmt.Fprintf(p.writer, "\nSummary: %s to install, %s to upgrade, %s to remove\n",
		p.installColor.Sprintf("%d", counts[ActionInstall]),
		p.upgradeColor.Sprintf("%d", counts[ActionUpgrade]),
		p.removeColor.Sprintf("%d", counts[ActionRemove]),
	)
}
