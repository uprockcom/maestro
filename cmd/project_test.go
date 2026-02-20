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
	"strings"
	"testing"
)

func TestProjectConfig_Validate_SinglePath(t *testing.T) {
	p := ProjectConfig{Path: "/some/path"}
	if err := p.Validate("test"); err != nil {
		t.Errorf("valid single-path should not error: %v", err)
	}
}

func TestProjectConfig_Validate_MultiPath(t *testing.T) {
	p := ProjectConfig{Paths: []string{"/a", "/b"}}
	if err := p.Validate("test"); err != nil {
		t.Errorf("valid multi-path should not error: %v", err)
	}
}

func TestProjectConfig_Validate_MutuallyExclusive(t *testing.T) {
	p := ProjectConfig{Path: "/a", Paths: []string{"/b"}}
	err := p.Validate("test")
	if err == nil {
		t.Error("expected error for path + paths")
	}
}

func TestProjectConfig_Validate_NeitherSet(t *testing.T) {
	p := ProjectConfig{}
	err := p.Validate("test")
	if err == nil {
		t.Error("expected error when neither path nor paths is set")
	}
}

func TestProjectConfig_Validate_PrimaryRequiresPaths(t *testing.T) {
	p := ProjectConfig{Path: "/a", Primary: "a"}
	err := p.Validate("test")
	if err == nil {
		t.Error("expected error: primary requires paths")
	}
}

func TestProjectConfig_Validate_PrimaryNotInPaths(t *testing.T) {
	p := ProjectConfig{Paths: []string{"/a", "/b"}, Primary: "c"}
	err := p.Validate("test")
	if err == nil {
		t.Error("expected error: primary not in paths")
	}
}

func TestProjectConfig_Validate_PrimaryInPaths(t *testing.T) {
	p := ProjectConfig{Paths: []string{"/workspace/repo-a", "/workspace/repo-b"}, Primary: "repo-b"}
	err := p.Validate("test")
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestProjectConfig_IsSinglePath(t *testing.T) {
	single := ProjectConfig{Path: "/a"}
	if !single.IsSinglePath() {
		t.Error("expected IsSinglePath true")
	}
	multi := ProjectConfig{Paths: []string{"/a"}}
	if multi.IsSinglePath() {
		t.Error("expected IsSinglePath false")
	}
}

func TestProjectConfig_PrimaryPath_Default(t *testing.T) {
	p := ProjectConfig{Paths: []string{"/workspace/first", "/workspace/second"}}
	if p.PrimaryPath() != "/workspace/first" {
		t.Errorf("expected first path as primary, got %q", p.PrimaryPath())
	}
}

func TestProjectConfig_PrimaryPath_Explicit(t *testing.T) {
	p := ProjectConfig{Paths: []string{"/workspace/first", "/workspace/second"}, Primary: "second"}
	if p.PrimaryPath() != "/workspace/second" {
		t.Errorf("expected /workspace/second, got %q", p.PrimaryPath())
	}
}

func TestProjectConfig_PrimaryPath_SinglePath(t *testing.T) {
	p := ProjectConfig{Path: "/workspace/myproject"}
	if p.PrimaryPath() != "/workspace/myproject" {
		t.Errorf("expected /workspace/myproject, got %q", p.PrimaryPath())
	}
}

// --- matchProject tests ---

func TestMatchProject_NoProjectFlag(t *testing.T) {
	proj, name, err := matchProject("/workspace", "", true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proj != nil || name != "" {
		t.Error("expected nil project for --no-project")
	}
}

func TestMatchProject_ExplicitFlag(t *testing.T) {
	projects := map[string]ProjectConfig{
		"myapp": {Path: "/home/user/myapp"},
	}
	proj, name, err := matchProject("/somewhere/else", "myapp", false, projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "myapp" {
		t.Errorf("expected name 'myapp', got %q", name)
	}
	if proj == nil || proj.Path != "/home/user/myapp" {
		t.Errorf("expected project config, got %v", proj)
	}
}

func TestMatchProject_ExplicitFlag_NotFound(t *testing.T) {
	projects := map[string]ProjectConfig{
		"myapp": {Path: "/home/user/myapp"},
	}
	_, _, err := matchProject("", "missing", false, projects)
	if err == nil {
		t.Fatal("expected error for missing project")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestMatchProject_ExplicitFlag_NoProjects(t *testing.T) {
	_, _, err := matchProject("", "myapp", false, nil)
	if err == nil {
		t.Fatal("expected error when no projects configured")
	}
}

func TestMatchProject_AutoDetect_SinglePath(t *testing.T) {
	projects := map[string]ProjectConfig{
		"myapp": {Path: "/workspace/myapp"},
	}
	proj, name, err := matchProject("/workspace/myapp", "", false, projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "myapp" || proj == nil {
		t.Errorf("expected auto-detected 'myapp', got name=%q proj=%v", name, proj)
	}
}

func TestMatchProject_AutoDetect_Subdirectory(t *testing.T) {
	projects := map[string]ProjectConfig{
		"myapp": {Path: "/workspace/myapp"},
	}
	proj, name, err := matchProject("/workspace/myapp/src/pkg", "", false, projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "myapp" || proj == nil {
		t.Errorf("expected auto-detected 'myapp' from subdir, got name=%q", name)
	}
}

func TestMatchProject_AutoDetect_LongestMatch(t *testing.T) {
	projects := map[string]ProjectConfig{
		"parent": {Path: "/workspace"},
		"child":  {Path: "/workspace/myapp"},
	}
	_, name, err := matchProject("/workspace/myapp/src", "", false, projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "child" {
		t.Errorf("expected longest match 'child', got %q", name)
	}
}

func TestMatchProject_AutoDetect_MultiPath(t *testing.T) {
	projects := map[string]ProjectConfig{
		"multi": {Paths: []string{"/workspace/repo-a", "/workspace/repo-b"}},
	}
	proj, name, err := matchProject("/workspace/repo-b/pkg", "", false, projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "multi" || proj == nil {
		t.Errorf("expected auto-detected 'multi', got name=%q", name)
	}
}

func TestMatchProject_AutoDetect_NoMatch(t *testing.T) {
	projects := map[string]ProjectConfig{
		"myapp": {Path: "/workspace/myapp"},
	}
	proj, name, err := matchProject("/home/user/other", "", false, projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proj != nil || name != "" {
		t.Error("expected nil project when cwd doesn't match")
	}
}

func TestMatchProject_AutoDetect_NilProjects(t *testing.T) {
	proj, name, err := matchProject("/workspace", "", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proj != nil || name != "" {
		t.Error("expected nil project with nil projects map")
	}
}

func TestMatchProject_AutoDetect_EmptyCwd(t *testing.T) {
	projects := map[string]ProjectConfig{
		"myapp": {Path: "/workspace/myapp"},
	}
	proj, _, err := matchProject("", "", false, projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proj != nil {
		t.Error("expected nil project with empty cwd")
	}
}

// --- Primary-aware matching tests ---

func TestMatchProject_AutoDetect_PrimaryWins(t *testing.T) {
	// Issue #12: when cwd is the primary of one project, that project wins
	// even if the same folder appears in another project.
	projects := map[string]ProjectConfig{
		"project-a": {
			Paths:   []string{"/foo/bar/folder-a", "/foo/bar/folder-b", "/foo/bar/folder-c"},
			Primary: "folder-a",
		},
		"project-c": {
			Paths:   []string{"/foo/bar/folder-a", "/foo/bar/folder-b", "/foo/bar/folder-c"},
			Primary: "folder-c",
		},
	}

	// cwd in folder-a → project-a (folder-a is project-a's primary)
	_, name, err := matchProject("/foo/bar/folder-a", "", false, projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "project-a" {
		t.Errorf("expected project-a for folder-a (its primary), got %q", name)
	}

	// cwd in folder-c → project-c (folder-c is project-c's primary)
	_, name, err = matchProject("/foo/bar/folder-c", "", false, projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "project-c" {
		t.Errorf("expected project-c for folder-c (its primary), got %q", name)
	}
}

func TestMatchProject_AutoDetect_PrimaryWins_Subdirectory(t *testing.T) {
	// Primary match should also work from subdirectories.
	projects := map[string]ProjectConfig{
		"project-a": {
			Paths:   []string{"/foo/bar/folder-a", "/foo/bar/folder-c"},
			Primary: "folder-a",
		},
		"project-c": {
			Paths:   []string{"/foo/bar/folder-a", "/foo/bar/folder-c"},
			Primary: "folder-c",
		},
	}
	_, name, err := matchProject("/foo/bar/folder-a/src/pkg", "", false, projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "project-a" {
		t.Errorf("expected project-a from subdirectory, got %q", name)
	}
}

func TestMatchProject_AutoDetect_NonPrimaryDeterministic(t *testing.T) {
	// folder-b is not primary in either project — alphabetical name breaks tie.
	projects := map[string]ProjectConfig{
		"project-a": {
			Paths:   []string{"/foo/bar/folder-a", "/foo/bar/folder-b"},
			Primary: "folder-a",
		},
		"project-c": {
			Paths:   []string{"/foo/bar/folder-b", "/foo/bar/folder-c"},
			Primary: "folder-c",
		},
	}
	// Neither project has folder-b as primary → alphabetical: project-a < project-c
	_, name, err := matchProject("/foo/bar/folder-b", "", false, projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "project-a" {
		t.Errorf("expected project-a (alphabetical tiebreak), got %q", name)
	}
}

func TestMatchProject_AutoDetect_LongerPrefixBeatsPrimary(t *testing.T) {
	// A longer path match should always win, even if the shorter match is primary.
	projects := map[string]ProjectConfig{
		"broad": {
			Paths:   []string{"/workspace", "/workspace/myapp"},
			Primary: "/workspace",
		},
		"specific": {Path: "/workspace/myapp"},
	}
	_, name, err := matchProject("/workspace/myapp/src", "", false, projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "specific" {
		t.Errorf("expected specific (longer prefix), got %q", name)
	}
}

func TestMatchProject_AutoDetect_SinglePathVsMultiPath(t *testing.T) {
	// Single-path project should win over multi-path when same folder, since
	// single-path is always treated as primary.
	projects := map[string]ProjectConfig{
		"multi": {
			Paths:   []string{"/workspace/repo-a", "/workspace/repo-b"},
			Primary: "repo-b",
		},
		"single": {Path: "/workspace/repo-a"},
	}
	_, name, err := matchProject("/workspace/repo-a", "", false, projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// single-path is treated as primary; multi's repo-a is not primary.
	// Both have same path length, primary wins → "single".
	// If both were primary (or both non-primary), alphabetical: "multi" < "single",
	// but primary beats non-primary so "single" wins.
	if name != "single" {
		t.Errorf("expected single (primary match), got %q", name)
	}
}

// --- isBetterMatch unit tests ---

func TestIsBetterMatch_LongerWins(t *testing.T) {
	if !isBetterMatch(20, false, "z", 10, true, "a") {
		t.Error("longer prefix should always win")
	}
}

func TestIsBetterMatch_SameLength_PrimaryWins(t *testing.T) {
	if !isBetterMatch(10, true, "z", 10, false, "a") {
		t.Error("primary should win over non-primary at same length")
	}
	if isBetterMatch(10, false, "a", 10, true, "z") {
		t.Error("non-primary should not beat primary at same length")
	}
}

func TestIsBetterMatch_SameLength_SamePrimary_AlphabeticalWins(t *testing.T) {
	if !isBetterMatch(10, false, "alpha", 10, false, "beta") {
		t.Error("alphabetically earlier name should win")
	}
	if isBetterMatch(10, false, "beta", 10, false, "alpha") {
		t.Error("alphabetically later name should not win")
	}
}

func TestIsPathPrimary(t *testing.T) {
	single := ProjectConfig{Path: "/workspace/app"}
	if !isPathPrimary(&single, "/workspace/app", "/workspace/app") {
		t.Error("single-path should always be primary")
	}

	multiNoPrimary := ProjectConfig{Paths: []string{"/a", "/b"}}
	if isPathPrimary(&multiNoPrimary, "/a", "/a") {
		t.Error("multi-path with no primary set should return false")
	}

	multiWithPrimary := ProjectConfig{Paths: []string{"/workspace/repo-a", "/workspace/repo-b"}, Primary: "repo-b"}
	if isPathPrimary(&multiWithPrimary, "/workspace/repo-a", "/workspace/repo-a") {
		t.Error("non-primary path should return false")
	}
	if !isPathPrimary(&multiWithPrimary, "/workspace/repo-b", "/workspace/repo-b") {
		t.Error("primary path should return true")
	}
}
