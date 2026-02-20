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
	"encoding/json"
	"os/exec"
	"strings"
)

// ReadContainerQuestion reads the current-question.json file from a container.
// Returns nil, nil if the file does not exist (not an error).
func ReadContainerQuestion(containerName string) (*QuestionData, error) {
	cmd := exec.Command("docker", "exec", containerName,
		"cat", "/home/node/.maestro/current-question.json")
	output, err := cmd.Output()
	if err != nil {
		// File doesn't exist or container issue — not an error for our purposes.
		return nil, nil
	}

	content := strings.TrimSpace(string(output))
	if content == "" {
		return nil, nil
	}

	var qd QuestionData
	if err := json.Unmarshal([]byte(content), &qd); err != nil {
		return nil, err
	}
	return &qd, nil
}
