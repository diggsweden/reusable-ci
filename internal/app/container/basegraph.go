// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Default workspace-relative locations of the base graph definition and
// the Containerfile its fragment inputs refer to.
const (
	DefaultBaseGraphFile          = "packaging/container/base-graph.json"
	DefaultBaseGraphContainerfile = "packaging/container/Containerfile"
)

// BaseGraphInput selects the workspace root, graph file, Containerfile and
// architecture set used to compute base-graph content IDs.
type BaseGraphInput struct {
	Root          string
	GraphFile     string
	Containerfile string
	ArchSet       []string
}

// BaseGraphInputsResult carries the per-flavor base inputs, their JSON
// encoding and the digest over that encoding (the input-set ID).
type BaseGraphInputsResult struct {
	InputsJSON string
	InputSetID string
	Inputs     []BaseInput
}

// BaseGraphSingleInputResult holds the content ID and base-input ID computed
// for one flavor.
type BaseGraphSingleInputResult struct {
	ContentID   string
	BaseInputID string
}

// BaseGraphGroupsForMissingInput selects which groups contain at least one
// missing flavor, from either an inline groups JSON or the graph file.
type BaseGraphGroupsForMissingInput struct {
	Root          string
	GraphFile     string
	GroupsJSON    string
	MissingFlavor []string
	MissingJSON   string
}

// BaseGraphGroupInput names one group in a base graph.
type BaseGraphGroupInput struct {
	Root      string
	GraphFile string
	Group     string
}

// BaseGraphGroup is one build group from the base graph: its flavors and the
// context files that invalidate the group when changed.
type BaseGraphGroup struct {
	Group        string   `json:"group"`
	Flavors      []string `json:"flavors"`
	ContextFiles []string `json:"context_files"`
}

type baseGraphDocument struct {
	CommonInputs []string `json:"common_inputs"`
	Flavors      []struct {
		Flavor  string   `json:"flavor"`
		Parents []string `json:"parents"`
		Inputs  []string `json:"inputs"`
	} `json:"flavors"`
	Groups []BaseGraphGroup `json:"groups"`
}

type baseGraphComputer struct {
	root          string
	graph         baseGraphDocument
	containerfile string
	archSet       string
	contentIDs    map[string]string
	baseInputIDs  map[string]string
	visiting      map[string]bool
}

// BaseGraphInputs computes the content ID and base-input ID for every flavor
// in the graph and returns them with the canonical JSON encoding and its
// input-set digest.
func BaseGraphInputs(in BaseGraphInput) (BaseGraphInputsResult, error) {
	computer, err := newBaseGraphComputer(in)
	if err != nil {
		return BaseGraphInputsResult{}, err
	}

	inputs := make([]BaseInput, 0, len(computer.graph.Flavors))
	for _, flavor := range computer.graph.Flavors {
		baseInputID, idErr := computer.computeManifestID(flavor.Flavor)
		if idErr != nil {
			return BaseGraphInputsResult{}, idErr
		}

		inputs = append(inputs, BaseInput{Flavor: flavor.Flavor, ContentID: computer.contentIDs[flavor.Flavor], BaseInputID: baseInputID})
	}

	body, err := json.Marshal(inputs)
	if err != nil {
		return BaseGraphInputsResult{}, fmt.Errorf("base graph: marshal inputs JSON: %w", err)
	}

	inputsJSON := string(body)
	inputSetID := baseGraphHexSHA256([]byte(inputsJSON + "\n"))

	return BaseGraphInputsResult{InputsJSON: inputsJSON, InputSetID: inputSetID, Inputs: inputs}, nil
}

// BaseGraphSingleInput computes the content ID and base-input ID for one
// flavor of the graph.
func BaseGraphSingleInput(in BaseGraphInput, flavor string) (BaseGraphSingleInputResult, error) {
	result, err := BaseGraphInputs(in)
	if err != nil {
		return BaseGraphSingleInputResult{}, err
	}

	for _, input := range result.Inputs {
		if input.Flavor == flavor {
			return BaseGraphSingleInputResult{ContentID: input.ContentID, BaseInputID: input.BaseInputID}, nil
		}
	}

	return BaseGraphSingleInputResult{}, fmt.Errorf("base graph: unknown flavor: %s: %w", flavor, errs.ErrValidation)
}

// BaseGraphGroups returns the graph's groups as a JSON array.
func BaseGraphGroups(root, graphFile string) (string, error) {
	graph, err := readBaseGraph(root, graphFile)
	if err != nil {
		return "", err
	}

	body, err := json.Marshal(graph.Groups)
	if err != nil {
		return "", fmt.Errorf("base graph: marshal groups JSON: %w", err)
	}

	return string(body), nil
}

// BaseGraphGroupFlavors returns the flavors of one named group.
func BaseGraphGroupFlavors(in BaseGraphGroupInput) ([]string, error) {
	group, err := baseGraphFindGroup(in.Root, in.GraphFile, in.Group)
	if err != nil {
		return nil, err
	}

	return slices.Clone(group.Flavors), nil
}

// BaseGraphGroupContextFiles returns the context files of one named group.
func BaseGraphGroupContextFiles(in BaseGraphGroupInput) ([]string, error) {
	group, err := baseGraphFindGroup(in.Root, in.GraphFile, in.Group)
	if err != nil {
		return nil, err
	}

	return slices.Clone(group.ContextFiles), nil
}

// BaseGraphGroupsForMissing returns, as a JSON array, the names of the
// groups that contain at least one missing flavor.
func BaseGraphGroupsForMissing(in BaseGraphGroupsForMissingInput) (string, error) {
	graphGroups, err := baseGraphGroupsForMissingInputGroups(in)
	if err != nil {
		return "", err
	}

	missing, err := baseGraphMissingSet(in.MissingFlavor, in.MissingJSON)
	if err != nil {
		return "", err
	}

	groups := make([]string, 0)

	for _, group := range graphGroups {
		for _, flavor := range group.Flavors {
			if missing[flavor] {
				groups = append(groups, group.Group)

				break
			}
		}
	}

	body, err := json.Marshal(groups)
	if err != nil {
		return "", fmt.Errorf("base graph: marshal groups-for-missing JSON: %w", err)
	}

	return string(body), nil
}

func baseGraphGroupsForMissingInputGroups(in BaseGraphGroupsForMissingInput) ([]BaseGraphGroup, error) {
	if strings.TrimSpace(in.GroupsJSON) == "" {
		graph, err := readBaseGraph(in.Root, in.GraphFile)
		if err != nil {
			return nil, err
		}

		return graph.Groups, nil
	}

	var groups []BaseGraphGroup
	if err := json.Unmarshal([]byte(in.GroupsJSON), &groups); err != nil {
		return nil, fmt.Errorf("base graph: groups JSON must be an array: %w: %w", err, errs.ErrMalformedInput)
	}

	if groups == nil {
		return nil, fmt.Errorf("base graph: groups JSON must be an array: %w", errs.ErrMalformedInput)
	}

	return groups, nil
}

func newBaseGraphComputer(in BaseGraphInput) (*baseGraphComputer, error) {
	root := baseGraphRoot(in.Root)
	graphFile := defaultBaseGraphString(in.GraphFile, DefaultBaseGraphFile)
	containerfile := defaultBaseGraphString(in.Containerfile, DefaultBaseGraphContainerfile)

	graph, err := readBaseGraph(root, graphFile)
	if err != nil {
		return nil, err
	}

	containerfilePath, err := baseGraphResolvePath(root, containerfile)
	if err != nil {
		return nil, fmt.Errorf("base graph: invalid containerfile path: %w", err)
	}

	if info, statErr := os.Stat(containerfilePath); statErr != nil || info.IsDir() {
		return nil, fmt.Errorf("base graph: Containerfile is missing: %s: %w", containerfile, errs.ErrMissingInput)
	}

	archSet, err := baseGraphArchSet(in.ArchSet)
	if err != nil {
		return nil, err
	}

	computer := &baseGraphComputer{
		root:          root,
		graph:         graph,
		containerfile: containerfile,
		archSet:       archSet,
		contentIDs:    map[string]string{},
		baseInputIDs:  map[string]string{},
		visiting:      map[string]bool{},
	}
	if err := computer.validate(); err != nil {
		return nil, err
	}

	return computer, nil
}

func readBaseGraph(root, graphFile string) (baseGraphDocument, error) {
	root = baseGraphRoot(root)
	graphFile = defaultBaseGraphString(graphFile, DefaultBaseGraphFile)

	path, err := baseGraphResolvePath(root, graphFile)
	if err != nil {
		return baseGraphDocument{}, fmt.Errorf("base graph: invalid graph path: %w", err)
	}

	body, err := os.ReadFile(path) //nolint:gosec // caller-selected graph path, constrained to the workspace root.
	if err != nil {
		return baseGraphDocument{}, fmt.Errorf("base graph: graph file is missing: %s: %w", graphFile, errs.ErrMissingInput)
	}

	var graph baseGraphDocument
	if err := json.Unmarshal(body, &graph); err != nil {
		return baseGraphDocument{}, fmt.Errorf("base graph: parse %s: %w: %w", graphFile, err, errs.ErrMalformedInput)
	}

	if len(graph.Flavors) == 0 {
		return baseGraphDocument{}, fmt.Errorf("base graph: graph contains no flavors: %w", errs.ErrValidation)
	}

	return graph, nil
}

func (c *baseGraphComputer) validate() error {
	seen := map[string]bool{}

	for _, flavor := range c.graph.Flavors {
		if !baseImageFlavorRE.MatchString(flavor.Flavor) {
			return fmt.Errorf("base graph: invalid flavor name: %s: %w", flavor.Flavor, errs.ErrValidation)
		}

		if seen[flavor.Flavor] {
			return fmt.Errorf("base graph: duplicate flavor: %s: %w", flavor.Flavor, errs.ErrValidation)
		}

		seen[flavor.Flavor] = true
	}

	for _, flavor := range c.graph.Flavors {
		for _, parent := range flavor.Parents {
			if !seen[parent] {
				return fmt.Errorf("base graph: flavor %s references unknown parent %s: %w", flavor.Flavor, parent, errs.ErrValidation)
			}
		}
	}

	return nil
}

func (c *baseGraphComputer) computeManifestID(flavor string) (string, error) {
	if id := c.baseInputIDs[flavor]; id != "" {
		return id, nil
	}

	contentID, err := c.computeContentID(flavor)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer

	_, _ = fmt.Fprintf(&buf, "base=%s\n", flavor)
	_, _ = fmt.Fprintln(&buf, "kind=manifest")
	_, _ = fmt.Fprintf(&buf, "content_id=%s\n", contentID)
	_, _ = fmt.Fprintf(&buf, "arch_set=%s\n", c.archSet)
	id := baseGraphHexSHA256(buf.Bytes())
	c.baseInputIDs[flavor] = id

	return id, nil
}

func (c *baseGraphComputer) computeContentID(flavor string) (string, error) {
	if id := c.contentIDs[flavor]; id != "" {
		return id, nil
	}

	if c.visiting[flavor] {
		return "", fmt.Errorf("base graph: parent cycle includes flavor %s: %w", flavor, errs.ErrValidation)
	}

	item, ok := c.flavor(flavor)
	if !ok {
		return "", fmt.Errorf("base graph: unknown flavor: %s: %w", flavor, errs.ErrValidation)
	}

	c.visiting[flavor] = true
	defer delete(c.visiting, flavor)

	for _, parent := range item.Parents {
		if _, err := c.computeContentID(parent); err != nil {
			return "", err
		}
	}

	var buf bytes.Buffer

	_, _ = fmt.Fprintf(&buf, "base=%s\n", flavor)

	_, _ = fmt.Fprintln(&buf, "kind=content")
	for _, parent := range item.Parents {
		_, _ = fmt.Fprintf(&buf, "parent:%s=%s\n", parent, c.contentIDs[parent])
	}

	if err := c.writeInputDigests(&buf, c.graph.CommonInputs); err != nil {
		return "", err
	}

	if err := c.writeInputDigests(&buf, item.Inputs); err != nil {
		return "", err
	}

	id := baseGraphHexSHA256(buf.Bytes())
	c.contentIDs[flavor] = id

	return id, nil
}

func (c *baseGraphComputer) flavor(name string) (struct {
	Flavor  string   `json:"flavor"`
	Parents []string `json:"parents"`
	Inputs  []string `json:"inputs"`
}, bool) {
	for _, flavor := range c.graph.Flavors {
		if flavor.Flavor == name {
			return flavor, true
		}
	}

	return struct {
		Flavor  string   `json:"flavor"`
		Parents []string `json:"parents"`
		Inputs  []string `json:"inputs"`
	}{}, false
}

// writeInputDigests folds every input's digest line into buf, in order.
func (c *baseGraphComputer) writeInputDigests(buf *bytes.Buffer, inputs []string) error {
	for _, input := range inputs {
		if err := c.writeInputDigest(buf, input); err != nil {
			return err
		}
	}

	return nil
}

func (c *baseGraphComputer) writeInputDigest(buf *bytes.Buffer, input string) error {
	switch {
	case strings.HasPrefix(input, "containerfile:arg:"):
		name := strings.TrimPrefix(input, "containerfile:arg:")

		fragment, err := c.containerfileArgLine(name)
		if err != nil {
			return err
		}

		_, _ = fmt.Fprintf(buf, "fragment=%s\nsha256=%s\n", input, baseGraphHexSHA256(fragment))

		return nil
	case strings.HasPrefix(input, "containerfile:stage:"):
		stage := strings.TrimPrefix(input, "containerfile:stage:")

		fragment, err := c.containerfileStage(stage)
		if err != nil {
			return err
		}

		_, _ = fmt.Fprintf(buf, "fragment=%s\nsha256=%s\n", input, baseGraphHexSHA256(fragment))

		return nil
	default:
		path, err := baseGraphResolvePath(c.root, input)
		if err != nil {
			return fmt.Errorf("base graph: invalid input path %s: %w", input, err)
		}

		body, err := os.ReadFile(path) //nolint:gosec // graph-selected path constrained to root.
		if err != nil {
			return fmt.Errorf("base graph: base image input is missing: %s: %w", input, errs.ErrMissingInput)
		}

		_, _ = fmt.Fprintf(buf, "path=%s\n%s  %s\n", input, baseGraphHexSHA256(body), input)

		return nil
	}
}

func (c *baseGraphComputer) containerfileArgLine(name string) ([]byte, error) {
	if name == "" {
		return nil, fmt.Errorf("base graph: containerfile ARG name is empty: %w", errs.ErrUsage)
	}

	path, err := baseGraphResolvePath(c.root, c.containerfile)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(path) //nolint:gosec // caller-selected Containerfile path, constrained to root.
	if err != nil {
		return nil, fmt.Errorf("base graph: open Containerfile %s: %w", c.containerfile, err)
	}

	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "ARG" {
			continue
		}

		argName, _, _ := strings.Cut(fields[1], "=")
		if argName == name {
			return []byte(line + "\n"), nil
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("base graph: read Containerfile %s: %w", c.containerfile, err)
	}

	return nil, fmt.Errorf("base graph: Containerfile lacks ARG %s: %w", name, errs.ErrValidation)
}

func (c *baseGraphComputer) containerfileStage(stage string) ([]byte, error) {
	if stage == "" {
		return nil, fmt.Errorf("base graph: containerfile stage name is empty: %w", errs.ErrUsage)
	}

	path, err := baseGraphResolvePath(c.root, c.containerfile)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(path) //nolint:gosec // caller-selected Containerfile path, constrained to root.
	if err != nil {
		return nil, fmt.Errorf("base graph: open Containerfile %s: %w", c.containerfile, err)
	}

	defer func() { _ = file.Close() }()

	fragment, found, err := collectContainerfileStage(file, stage)
	if err != nil {
		return nil, fmt.Errorf("base graph: read Containerfile %s: %w", c.containerfile, err)
	}

	if !found {
		return nil, fmt.Errorf("base graph: Containerfile lacks stage %s: %w", stage, errs.ErrValidation)
	}

	return fragment, nil
}

// collectContainerfileStage scans a Containerfile and returns the lines of
// the named build stage (from its FROM line up to the next FROM), plus
// whether the stage was found at all.
func collectContainerfileStage(file io.Reader, stage string) ([]byte, bool, error) {
	var out bytes.Buffer

	found := false
	inStage := false

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if baseGraphIsUpperFromLine(line) {
			if inStage {
				break
			}

			inStage = false
			if baseGraphFromLineNamesStage(line, stage) {
				inStage = true
				found = true
			}
		}

		if inStage {
			_, _ = fmt.Fprintln(&out, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, false, err
	}

	return out.Bytes(), found, nil
}

func baseGraphFindGroup(root, graphFile, name string) (BaseGraphGroup, error) {
	if name == "" {
		return BaseGraphGroup{}, fmt.Errorf("base graph: group is required: %w", errs.ErrUsage)
	}

	graph, err := readBaseGraph(root, graphFile)
	if err != nil {
		return BaseGraphGroup{}, err
	}

	for _, group := range graph.Groups {
		if group.Group == name {
			return group, nil
		}
	}

	return BaseGraphGroup{}, fmt.Errorf("base graph: unsupported group: %s: %w", name, errs.ErrValidation)
}

func baseGraphMissingSet(flavors []string, raw string) (map[string]bool, error) {
	missing := map[string]bool{}

	for _, flavor := range flavors {
		flavor = strings.TrimSpace(flavor)
		if flavor != "" {
			missing[flavor] = true
		}
	}

	if strings.TrimSpace(raw) == "" {
		return missing, nil
	}

	var items []string
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("base graph: missing JSON must be an array of strings: %w: %w", err, errs.ErrMalformedInput)
	}

	for _, flavor := range items {
		if flavor != "" {
			missing[flavor] = true
		}
	}

	return missing, nil
}

func baseGraphRoot(root string) string {
	if strings.TrimSpace(root) == "" {
		return "."
	}

	return root
}

func baseGraphResolvePath(root, path string) (string, error) {
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\n\r") {
		return "", fmt.Errorf("unsafe or empty relative path %q: %w", path, errs.ErrUsage)
	}

	clean := filepath.Clean(path)
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", fmt.Errorf("path must stay under root: %s: %w", path, errs.ErrUsage)
	}

	return filepath.Join(root, clean), nil
}

func baseGraphArchSet(values []string) (string, error) {
	arches := baseGraphSplitArches(values)
	if len(arches) == 0 {
		arches = append(arches, archAMD64)
	}

	for _, arch := range arches {
		if !baseImageFlavorRE.MatchString(arch) {
			return "", fmt.Errorf("base graph: invalid arch name: %s: %w", arch, errs.ErrValidation)
		}
	}

	return strings.Join(arches, ","), nil
}

// baseGraphSplitArches splits arch-set values on commas and whitespace,
// dropping empties.
func baseGraphSplitArches(values []string) []string {
	arches := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, isArchSetSeparator) {
			if part != "" {
				arches = append(arches, part)
			}
		}
	}

	return arches
}

func isArchSetSeparator(r rune) bool {
	return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == '\r'
}

func baseGraphIsUpperFromLine(line string) bool {
	return strings.HasPrefix(line, "FROM ") || strings.HasPrefix(line, "FROM\t")
}

func baseGraphFromLineNamesStage(line, stage string) bool {
	fields := strings.Fields(line)
	for idx := 0; idx+1 < len(fields); idx++ {
		if strings.EqualFold(fields[idx], "AS") && fields[idx+1] == stage {
			return true
		}
	}

	return false
}

func baseGraphHexSHA256(body []byte) string {
	sum := sha256.Sum256(body)

	return hex.EncodeToString(sum[:])
}

func defaultBaseGraphString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}

	return value
}
