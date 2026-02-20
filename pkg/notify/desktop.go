// Copyright 2025 Christopher O'Connell
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

package notify

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// DesktopProvider sends macOS/Linux desktop notifications.
type DesktopProvider struct {
	iconPath            string
	hasTerminalNotifier bool
}

// NewDesktopProvider creates a desktop notification provider.
func NewDesktopProvider(iconPath string, hasTerminalNotifier bool) *DesktopProvider {
	return &DesktopProvider{
		iconPath:            iconPath,
		hasTerminalNotifier: hasTerminalNotifier,
	}
}

func (d *DesktopProvider) Name() string { return "desktop" }

func (d *DesktopProvider) Send(_ context.Context, event Event) error {
	title := event.Title
	subtitle := event.ShortName
	message := event.Message

	switch runtime.GOOS {
	case "darwin":
		return d.sendDarwin(title, subtitle, message)
	case "linux":
		return d.sendLinux(title, subtitle, message)
	default:
		return fmt.Errorf("desktop notifications not supported on %s", runtime.GOOS)
	}
}

func (d *DesktopProvider) SendInteractive(_ context.Context, _ Event) (<-chan Response, bool, error) {
	return nil, false, ErrNotInteractive
}

func (d *DesktopProvider) Available() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "linux"
}

func (d *DesktopProvider) Close() error { return nil }

func (d *DesktopProvider) sendDarwin(title, subtitle, message string) error {
	if d.hasTerminalNotifier {
		args := []string{
			"-title", fmt.Sprintf("Maestro - %s", title),
			"-message", message,
		}
		if subtitle != "" {
			args = append(args, "-subtitle", subtitle)
		}
		if d.iconPath != "" {
			args = append(args, "-contentImage", d.iconPath)
		}
		cmd := exec.Command("terminal-notifier", args...)
		if err := cmd.Run(); err == nil {
			return nil
		}
		// Fall through to osascript
	}

	if subtitle != "" {
		cmd := exec.Command("osascript",
			"-e", `on run argv`,
			"-e", `display notification (item 1 of argv) with title ("Maestro - " & item 2 of argv) subtitle (item 3 of argv)`,
			"-e", `end run`,
			"--",
			message, title, subtitle,
		)
		return cmd.Run()
	}

	cmd := exec.Command("osascript",
		"-e", `on run argv`,
		"-e", `display notification (item 1 of argv) with title ("Maestro - " & item 2 of argv)`,
		"-e", `end run`,
		"--",
		message, title,
	)
	return cmd.Run()
}

func (d *DesktopProvider) sendLinux(title, subtitle, message string) error {
	var args []string
	if d.iconPath != "" {
		args = append(args, "--icon", d.iconPath)
	}
	displayMsg := message
	if subtitle != "" {
		displayMsg = fmt.Sprintf("[%s] %s", subtitle, message)
	}
	args = append(args, fmt.Sprintf("Maestro - %s", title), displayMsg)
	cmd := exec.Command("notify-send", args...)
	return cmd.Run()
}
