package graph

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/fatih/color"

	"github.com/terassyi/tomei/internal/resource"
)

// ResourceInfo holds information about a resource for display.
type ResourceInfo struct {
	Kind    resource.Kind
	Name    string
	Version string
	Action  resource.ActionType
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

// newColor creates a *color.Color and disables it when noColor is true.
func newColor(noColor bool, attrs ...color.Attribute) *color.Color {
	c := color.New(attrs...)
	if noColor {
		c.DisableColor()
	}
	return c
}

// NewTreePrinter creates a new TreePrinter.
func NewTreePrinter(w io.Writer, noColor bool) *TreePrinter {
	return &TreePrinter{
		noColor:        noColor,
		writer:         w,
		installColor:   newColor(noColor, color.FgGreen),
		upgradeColor:   newColor(noColor, color.FgYellow),
		reinstallColor: newColor(noColor, color.FgCyan),
		removeColor:    newColor(noColor, color.FgRed),
		noChangeColor:  newColor(noColor, color.FgWhite),
		kindColors: map[resource.Kind]*color.Color{
			resource.KindRuntime:             newColor(noColor, color.FgBlue),
			resource.KindInstaller:           newColor(noColor, color.FgYellow),
			resource.KindInstallerRepository: newColor(noColor, color.FgCyan),
			resource.KindTool:                newColor(noColor, color.FgGreen),
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
		case resource.ActionInstall:
			actionStr = " [+ install]"
			actionColor = p.installColor
		case resource.ActionUpgrade:
			actionStr = " [~ upgrade]"
			actionColor = p.upgradeColor
		case resource.ActionReinstall:
			actionStr = " [↻ reinstall]"
			actionColor = p.reinstallColor
		case resource.ActionRemove:
			actionStr = " [- remove]"
			actionColor = p.removeColor
		case resource.ActionNone:
			actionColor = p.noChangeColor
		}

		if actionColor != nil && info.Action != resource.ActionNone {
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
	counts := map[resource.ActionType]int{
		resource.ActionInstall:   0,
		resource.ActionUpgrade:   0,
		resource.ActionReinstall: 0,
		resource.ActionRemove:    0,
	}

	for _, info := range resourceInfo {
		if info.Action != resource.ActionNone {
			counts[info.Action]++
		}
	}

	fmt.Fprintf(p.writer, "\nSummary: %s to install, %s to upgrade, %s to reinstall, %s to remove\n",
		p.installColor.Sprintf("%d", counts[resource.ActionInstall]),
		p.upgradeColor.Sprintf("%d", counts[resource.ActionUpgrade]),
		p.reinstallColor.Sprintf("%d", counts[resource.ActionReinstall]),
		p.removeColor.Sprintf("%d", counts[resource.ActionRemove]),
	)
}
