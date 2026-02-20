// Copyright 2026 Christopher O'Connell
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProjectConfig defines a named project with one or more repository paths.
type ProjectConfig struct {
	Path    string   `mapstructure:"path"`    // Single-path project → /workspace/
	Paths   []string `mapstructure:"paths"`   // Multi-path project → /workspace/<basename>/
	Primary string   `mapstructure:"primary"` // Optional: primary repo in Paths (default: first)
}

// Validate checks that the project config is consistent.
func (p *ProjectConfig) Validate(name string) error {
	if p.Path != "" && len(p.Paths) > 0 {
		return fmt.Errorf("project %q: 'path' and 'paths' are mutually exclusive", name)
	}
	if p.Path == "" && len(p.Paths) == 0 {
		return fmt.Errorf("project %q: must specify 'path' or 'paths'", name)
	}
	if p.Primary != "" && len(p.Paths) == 0 {
		return fmt.Errorf("project %q: 'primary' requires 'paths'", name)
	}
	if p.Primary != "" {
		found := false
		for _, pp := range p.Paths {
			if pp == p.Primary || filepath.Base(expandPath(pp)) == p.Primary {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("project %q: primary %q not found in paths", name, p.Primary)
		}
	}
	return nil
}

// IsSinglePath returns true if this is a single-path project.
func (p *ProjectConfig) IsSinglePath() bool {
	return p.Path != ""
}

// ExpandedPath returns the expanded single path.
func (p *ProjectConfig) ExpandedPath() string {
	return expandPath(p.Path)
}

// ExpandedPaths returns all expanded paths for a multi-path project.
func (p *ProjectConfig) ExpandedPaths() []string {
	result := make([]string, len(p.Paths))
	for i, p := range p.Paths {
		result[i] = expandPath(p)
	}
	return result
}

// PrimaryPath returns the primary repo path (expanded), defaulting to the first path.
func (p *ProjectConfig) PrimaryPath() string {
	if p.IsSinglePath() {
		return p.ExpandedPath()
	}
	if p.Primary != "" {
		for _, pp := range p.Paths {
			expanded := expandPath(pp)
			if pp == p.Primary || filepath.Base(expanded) == p.Primary {
				return expanded
			}
		}
	}
	if len(p.Paths) > 0 {
		return expandPath(p.Paths[0])
	}
	return ""
}

// resolveProject resolves the project to use based on flags and cwd.
// Returns (projectConfig, projectName, error).
// If no project matches, returns (nil, "", nil) for ad-hoc mode.
func resolveProject(flagProject string, flagNoProject bool) (*ProjectConfig, string, error) {
	cwd, _ := os.Getwd()
	if cwd != "" {
		cwd, _ = filepath.Abs(cwd)
	}
	return matchProject(cwd, flagProject, flagNoProject, config.Projects)
}

// isBetterMatch determines whether a new candidate project match should replace
// the current best. Priority order: (1) longer path prefix, (2) primary match
// over non-primary, (3) alphabetical project name for determinism.
func isBetterMatch(newLen int, newIsPrimary bool, newName string, bestLen int, bestIsPrimary bool, bestName string) bool {
	if newLen != bestLen {
		return newLen > bestLen
	}
	if newIsPrimary != bestIsPrimary {
		return newIsPrimary
	}
	return newName < bestName
}

// isPathPrimary checks whether a raw path entry is the primary of its project.
func isPathPrimary(proj *ProjectConfig, rawPath, expandedPath string) bool {
	if proj.IsSinglePath() {
		return true
	}
	if proj.Primary == "" {
		return false
	}
	return rawPath == proj.Primary || filepath.Base(expandedPath) == proj.Primary
}

// matchProject contains the pure matching logic for project resolution.
// It takes explicit parameters instead of reading globals, making it testable.
func matchProject(cwd, flagProject string, flagNoProject bool, projects map[string]ProjectConfig) (*ProjectConfig, string, error) {
	// --no-project forces ad-hoc
	if flagNoProject {
		return nil, "", nil
	}

	// -p <name> explicitly selects a project
	if flagProject != "" {
		if projects == nil {
			return nil, "", fmt.Errorf("no projects configured; add projects to config.yml")
		}
		proj, ok := projects[flagProject]
		if !ok {
			var names []string
			for k := range projects {
				names = append(names, k)
			}
			return nil, "", fmt.Errorf("project %q not found; available: %s", flagProject, strings.Join(names, ", "))
		}
		if err := proj.Validate(flagProject); err != nil {
			return nil, "", err
		}
		return &proj, flagProject, nil
	}

	// Auto-detect: check if cwd matches any project path
	if projects == nil {
		return nil, "", nil
	}

	if cwd == "" {
		return nil, "", nil // can't detect, fall through to ad-hoc
	}

	var bestName string
	var bestProj *ProjectConfig
	bestLen := 0
	bestIsPrimary := false

	for name, proj := range projects {
		p := proj // copy for pointer
		// Check single path
		if p.Path != "" {
			expanded, _ := filepath.Abs(expandPath(p.Path))
			isPrimary := true // single-path is always primary
			if strings.HasPrefix(cwd, expanded) && isBetterMatch(len(expanded), isPrimary, name, bestLen, bestIsPrimary, bestName) {
				bestLen = len(expanded)
				bestName = name
				bestProj = &p
				bestIsPrimary = isPrimary
			}
		}
		// Check all multi-paths
		for _, pp := range p.Paths {
			expanded, _ := filepath.Abs(expandPath(pp))
			isPrimary := isPathPrimary(&p, pp, expanded)
			if strings.HasPrefix(cwd, expanded) && isBetterMatch(len(expanded), isPrimary, name, bestLen, bestIsPrimary, bestName) {
				bestLen = len(expanded)
				bestName = name
				bestProj = &p
				bestIsPrimary = isPrimary
			}
		}
	}

	if bestProj != nil {
		if err := bestProj.Validate(bestName); err != nil {
			return nil, "", err
		}
		return bestProj, bestName, nil
	}

	return nil, "", nil
}
