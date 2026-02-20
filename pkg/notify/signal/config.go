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

package signal

// Config holds the configuration for the Signal notification provider.
type Config struct {
	Number    string // bot's registered Signal phone number
	Recipient string // user's phone number (receives notifications)
	Port      int    // localhost port for signal-cli container (default 8080)
	URL       string // remote relay URL — if set, skip local Docker
	APIKey    string // API key for remote relay auth
}
