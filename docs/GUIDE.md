# Maestro Usage Guide

Complete documentation for Maestro (Multi-Container Claude), covering advanced features, configuration, architecture, and troubleshooting.

## Table of Contents

- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [Background Daemon](#background-daemon)
- [Token Management](#token-management)
- [Network Management](#network-management)
- [Architecture](#architecture)
- [Troubleshooting](#troubleshooting)
- [Development](#development)

## Installation

### Quick Install (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/uprockcom/maestro/main/install.sh | bash
```

This will:
- Download the latest release for your platform
- Install the `maestro` binary to `/usr/local/bin`
- Pull the Docker image
- Create the config directory at `~/.maestro`
- Set up the example configuration

**Prerequisites:**
- Docker must be installed and running
- `curl` command available

### Build from Source

For development or if you prefer to build from source:

```bash
# Clone the repository
git clone https://github.com/uprockcom/maestro.git
cd maestro

# Build everything (binary + Docker image)
make all

# Install to /usr/local/bin (requires sudo)
sudo make install
```

Available make targets:
```bash
make build          # Build maestro binary only
make docker         # Build Docker image only
make all            # Build both binary and image
make install        # Install maestro to /usr/local/bin
make test           # Run Go tests
make clean          # Remove built binaries
make help           # Show all available targets
```

### Post-Installation Setup

After installation, set up authentication:

```bash
maestro auth
```

Configure your preferences:
```bash
nano ~/.maestro/config.yml
```

## Configuration

The configuration file lives at `~/.maestro/config.yml`. Here's a complete reference:

```yaml
claude:
  config_path: ~/.claude       # Your Claude auth directory
  mcl_claude_path: ~/.maestro/.claude  # Maestro's centralized auth storage
  default_mode: yolo           # Auto-approve mode

containers:
  prefix: mcl-                 # Container name prefix
  image: mcl:latest            # Docker image name
  resources:
    memory: 4g                 # Memory limit
    cpus: "2"                  # CPU limit

firewall:
  allowed_domains:             # Whitelisted domains
    - github.com
    - pypi.org
    - api.anthropic.com
    # Add your domains here

sync:
  additional_folders:          # Folders to copy as siblings
    - ~/Documents/Code/mcp-servers
    - ~/Documents/Code/helpers
  compress: true               # Set to false for faster copying of large projects

github:
  enabled: false               # Enable GitHub CLI (gh) integration
  config_path: ~/.maestro/gh   # Path to gh config directory

daemon:
  check_interval: 30m          # How often to check containers
  show_nag: true               # Show reminder to start daemon
  token_refresh:
    enabled: true              # Auto-refresh expiring tokens
    threshold: 6h              # Refresh when < 6h remaining
  notifications:
    enabled: true              # Send desktop notifications
    attention_threshold: 5m    # Wait 5m before notifying
    notify_on:
      - attention_needed       # Notify when container needs attention
      - token_expiring         # Notify when token < 1h
    quiet_hours:
      start: "23:00"           # Optional: quiet hours start (24h format)
      end: "08:00"             # Optional: quiet hours end
```

### Configuration Notes

- **show_nag**: Set to `false` to disable the "start daemon" reminder in `maestro list`
- **check_interval**: How often the daemon checks containers (e.g., "30m", "1h", "15m")
- **attention_threshold**: How long to wait before sending notification (prevents spam)
- **quiet_hours**: Optional. Leave empty (`""`) to disable quiet hours
- Time formats: Use Go duration format ("30m", "6h") or 24-hour time ("23:00")

## Usage

### Creating Containers

```bash
# Quick task description
maestro new "implement OAuth authentication"

# From a specification file
maestro new -f specs/feature-design.md

# Interactive mode
maestro new
```

This will:
1. Use Claude to generate an appropriate branch name
2. Create a new container with incremented numbering (e.g., `maestro-feat-oauth-1`)
3. Copy your entire project into the container
4. Create and checkout the new git branch
5. Start tmux with Claude in planning mode
6. Connect you to the container

### Managing Containers

```bash
# List all containers with status indicators
maestro list        # or: maestro ls, maestro ps

# Connect to a container
maestro connect feat-oauth-1

# Restart a crashed Claude process (preserves container state)
maestro restart feat-oauth-1

# Full container restart (if needed)
maestro restart feat-oauth-1 --full

# Stop a specific container
maestro stop feat-oauth-1

# Stop all dormant containers (where Claude has exited)
maestro stop

# Clean up stopped containers and their volumes
maestro cleanup

# Remove all containers (including running) and their volumes
maestro cleanup --all

# Clean up orphaned volumes (volumes without containers)
maestro cleanup-volumes
```

### Container Status Indicators

The `maestro list` command shows comprehensive status:

```
NAME              STATUS   BRANCH         GIT      ACTIVITY  AUTH
----              ------   ------         ---      --------  ----
feat-oauth-1      running  feat/oauth     Δ79 ↑2   2m ago    ✓ 147.2h  🔔
fix-api-bug-1     running  fix/api-bug    ✓        5m ago    ⚠ 2.3h
refactor-db-1     running  refactor/db    Δ5 ↓1    12h ago   ✗ EXPIRED 💤
```

**Status Indicators:**
- **GIT**:
  - `Δ79` = 79 changed files
  - `↑2` = 2 commits ahead of remote
  - `↓1` = 1 commit behind remote
  - `✓` = clean working tree
- **AUTH**:
  - `✓ Xh` = Token valid for X hours (green)
  - `⚠ Xh` = Token expires in < 24 hours (yellow warning)
  - `✗ EXPIRED` = Token has expired (red)
- **🔔**: Container needs attention (tmux bell detected)
- **💤**: Container is dormant (Claude process has exited)

### Inside the Container

When connected to a container via `maestro connect`:

- **Window 0**: Claude Code running in auto-approve mode
- **Window 1**: Shell for manual commands
- **Switch windows**: `Ctrl+b 0` (Claude) or `Ctrl+b 1` (shell)
- **Detach**: `Ctrl+b d` (returns you to host, container keeps running)

The tmux status line shows:
- Container name
- Current git branch
- Bell indicator when Claude needs attention

## Background Daemon

Maestro includes a background daemon that monitors your containers for token expiry and attention needs.

### Starting the Daemon

```bash
# Start the daemon (runs in background)
maestro daemon start

# Check daemon status
maestro daemon status

# View daemon logs
maestro daemon logs

# Stop the daemon
maestro daemon stop
```

### Daemon Features

**Token Monitoring**: Automatically checks token expiration and warns when tokens are expiring (< 1 hour remaining). Future versions will support automatic token refresh.

**Smart Notifications**: Only notifies after containers have needed attention for a configurable threshold (default 5 minutes), preventing notification spam.

**Quiet Hours**: Configure time ranges when notifications should be suppressed (e.g., 23:00-08:00).

**Activity Tracking**: Monitors container activity and Claude process health.

**Custom Notification Icons**: On macOS, install `terminal-notifier` for custom icon support:
```bash
brew install terminal-notifier
```

Without `terminal-notifier`, notifications still work via macOS's built-in `osascript`, but will use the Terminal/iTerm icon.

### Daemon Configuration

The daemon behavior is controlled by the `daemon` section in `~/.maestro/config.yml`:

- **check_interval**: How frequently to check containers (default: 30m)
- **show_nag**: Show reminder in `maestro list` if daemon isn't running (default: true)
- **notifications.enabled**: Enable/disable desktop notifications (default: true)
- **notifications.attention_threshold**: Wait time before notifying (default: 5m)
- **notifications.quiet_hours**: Optional time range to suppress notifications

## Token Management

Claude authentication tokens automatically expire after approximately 1 week. Maestro provides comprehensive tools to manage token expiration.

### Checking Token Status

The `maestro list` command shows authentication status for each running container:

```bash
maestro list
```

Output example:
```
NAME                STATUS   BRANCH           AUTH STATUS  ATTENTION
----                ------   ------           -----------  ---------
feat-oauth-1        running  feat/oauth       ✓ 147.2h
fix-api-bug-1       running  fix/api-bug      ⚠ 2.3h
refactor-db-1       running  refactor/db      ✗ EXPIRED    💤 DORMANT
```

### Refreshing Tokens

Claude CLI automatically refreshes tokens when actively used in a container. Use `maestro refresh-tokens` to find and propagate the freshest token:

```bash
maestro refresh-tokens
```

This command:
1. Scans all running containers and the host for credentials
2. Finds the container with the freshest token (Claude auto-refreshes during normal use)
3. Copies the fresh token to all other containers and the host
4. Ensures new containers will use the fresh token

**Example output:**
```
Scanning for credentials...
  ✓ Host: EXPIRED 2.8h ago
  ✓ maestro-feat-oauth-1: Valid for 147.2h
  ✓ maestro-fix-api-bug-1: EXPIRED 2.8h ago

✓ Found fresh token in maestro-feat-oauth-1
  Expires: Sat, 18 Oct 2025 06:33:27 PDT
  Status: Valid for 147.2h

Syncing credentials...
  ✓ Synced to host
  ✓ Synced to maestro-fix-api-bug-1

✅ Refresh complete! Synced to 2 location(s).
```

### Re-authenticating

If all tokens are expired, `maestro refresh-tokens` will prompt you to run `maestro auth`:

```bash
maestro auth
```

This will:
1. Start a temporary authentication container
2. Complete OAuth flow in your browser
3. Automatically sync new credentials to all running containers

### Token Expiration Warnings

When creating a new container, Maestro will warn you if tokens are expired or expiring soon:

```bash
maestro new "implement feature"

⚠️  WARNING: Authentication token is EXPIRED!
   Status: EXPIRED 2.8h ago
   Run 'maestro auth' or 'maestro refresh-tokens' to get a fresh token.

Continue creating container with expired token? (y/N):
```

### Best Practices

- **Check token status regularly**: Run `maestro list` to see auth status for all containers
- **Use `refresh-tokens` first**: If you see expired tokens, try `maestro refresh-tokens` before running `maestro auth` (it's faster and reuses existing fresh tokens)
- **Run `auth` when needed**: Only run `maestro auth` if all tokens are expired or `refresh-tokens` fails
- **Monitor expiration warnings**: If you see "⚠" warnings in `maestro list`, consider refreshing tokens soon

## Network Management

Containers include a firewall that restricts network access to whitelisted domains only.

### Adding Domains

```bash
# Add a domain temporarily to a running container
maestro add-domain feat-oauth-1 api.example.com

# The tool will offer to add it to ~/.maestro/config.yml for permanent access
```

### Firewall Configuration

Edit `~/.maestro/config.yml` to manage the domain whitelist:

```yaml
firewall:
  allowed_domains:
    - github.com
    - pypi.org
    - api.anthropic.com
    - your-domain.com
```

### What's Allowed by Default

Containers can access:
- Whitelisted domains from config
- GitHub API endpoints (auto-detected from git remotes)
- Local Docker network
- DNS resolution (port 53)

## Architecture

### Container Structure

```
/workspace/                     # Your main project (copied from host)
/workspace/../mcp-servers/      # Additional folders (from config)
/home/node/.claude/             # Claude config (mounted read-only)
  .credentials.json             # Shared OAuth credentials
  .claude.json                  # Container-specific state
```

### Persistent Volumes

Each container has named volumes for:
- **npm cache** (`<container>-npm`): Speeds up Node.js package installation
- **UV cache** (`<container>-uv`): Speeds up Python package installation
- **Command history** (`<container>-history`): Preserves bash/zsh history

These volumes persist across container restarts but are removed with `maestro cleanup`.

### Authentication Architecture

**Host (macOS)**: Credentials stored in keychain + `~/.maestro/.claude/.credentials.json`

**Containers (Linux)**: File-based authentication only. Each container:
1. Mounts `~/.maestro/.claude/` read-only
2. Copies `.credentials.json` to container-specific location
3. Generates its own `.claude.json` state file

This ensures:
- Single OAuth flow on the host
- Complete isolation between containers
- No credential conflicts

### Network Firewall

Containers launch with `NET_ADMIN` capability to manage iptables rules:

1. **Initialization**: `init-firewall.sh` runs at container startup
2. **Custom chain**: Creates `DOCKER_MCL_FILTER` iptables chain
3. **Default policy**: DROP all outgoing traffic
4. **Whitelist**: Allow configured domains + GitHub API
5. **Dynamic updates**: `maestro add-domain` modifies running containers

## Troubleshooting

### Container won't start

Check Docker logs:
```bash
docker logs maestro-feat-name-1
```

Common issues:
- Docker daemon not running
- Insufficient resources (memory/CPU)
- Port conflicts

### Firewall blocking needed domain

Add it temporarily:
```bash
maestro add-domain container-name api.example.com
```

Then add to `~/.maestro/config.yml` for permanent access.

### Can't connect to container

Ensure it's running:
```bash
docker ps
docker start <container-name>
maestro connect <container-name>
```

### Claude not authenticated

Check authentication status:
```bash
maestro list  # Shows auth status
maestro refresh-tokens  # Sync fresh tokens
maestro auth  # Re-authenticate if needed
```

Verify credentials exist:
```bash
ls -la ~/.maestro/.claude/.credentials.json
```

### Notifications not working

On macOS, install `terminal-notifier` for better notifications:
```bash
brew install terminal-notifier
```

Check daemon is running:
```bash
maestro daemon status
```

Check notification settings in `~/.maestro/config.yml`:
```yaml
daemon:
  notifications:
    enabled: true
```

### Token keeps expiring

Claude tokens expire after ~1 week. Best practices:

1. **Start the daemon**: `maestro daemon start` - monitors and warns about expiration
2. **Use refresh-tokens regularly**: `maestro refresh-tokens` syncs fresh tokens from active containers
3. **Re-authenticate when needed**: `maestro auth` when all tokens expire

## Development

### Modifying Maestro

To modify the maestro binary:

```bash
# Make changes to Go files

# Run tests
make test

# Rebuild binary
make build

# Test changes
./bin/maestro --help

# Install updated binary
make install
```

### Modifying the Container Image

To modify the container environment:

```bash
# Edit docker/Dockerfile

# Rebuild image
make docker

# Or rebuild everything
make all
```

### Project Structure

```
maestro/
├── cmd/                    # Cobra commands
│   ├── new.go             # Container creation
│   ├── list.go            # Status display
│   ├── connect.go         # Container connection
│   ├── auth.go            # Authentication
│   ├── daemon.go          # Background daemon
│   └── ...
├── pkg/                    # Internal packages
│   ├── container/         # Container operations
│   ├── daemon/            # Daemon implementation
│   ├── tui/               # Terminal UI components
│   ├── version/           # Version management
│   └── paths/             # Path utilities
├── docker/                 # Container images
│   ├── Dockerfile         # Main container image
│   └── signing/           # Code signing tools
├── scripts/                # Build and release scripts
└── Makefile               # Build automation
```

### Testing

Run the test suite:
```bash
make test
```

Test specific packages:
```bash
go test ./pkg/version/
go test ./pkg/paths/
```

### Release Process

Maestro uses GoReleaser for automated releases:

```bash
# Create a new release
make release VERSION=v1.2.3

# Test release build without publishing
make release-snapshot
```

The release process:
1. Runs preflight checks (git status, credentials, etc.)
2. Builds binaries for all platforms (Linux, macOS, Windows)
3. Signs macOS and Windows binaries
4. Creates GitHub release with changelog
5. Uploads binaries and checksums

## Tips and Best Practices

### Working with Multiple Versions

The numbering system (`-1`, `-2`, etc.) lets you create multiple implementations:

```bash
maestro new "implement auth with OAuth"     # Creates maestro-feat-auth-oauth-1
maestro new "implement auth with JWT"       # Creates maestro-feat-auth-jwt-1
maestro new "implement auth with OAuth"     # Creates maestro-feat-auth-oauth-2
```

### Monitoring Activity

Use `maestro list` frequently to:
- Check which containers need attention (🔔)
- Monitor token expiration
- See git status at a glance
- Identify dormant containers (💤)

Clean up dormant containers quickly:
```bash
maestro stop  # Stops all dormant containers
```

### Efficient Container Management

**Start the daemon**: Run `maestro daemon start` for automatic monitoring and notifications.

**Use refresh-tokens**: Before running `maestro auth`, try `maestro refresh-tokens` to reuse fresh tokens from active containers.

**Clean up regularly**: Run `maestro cleanup` to remove stopped containers and free disk space.

### GitHub CLI Integration

When you run `maestro auth`, you'll be prompted to set up GitHub CLI. This enables features like:
- `gh pr review 123`
- `gh issue list`
- `gh repo view`

To enable GitHub CLI in containers, set in `~/.maestro/config.yml`:
```yaml
github:
  enabled: true
  config_path: ~/.maestro/gh  # Managed by maestro auth
```

## Security Considerations

### Network Isolation

Containers are firewalled by default. Only whitelisted domains are accessible. This prevents:
- Accidental data exfiltration
- Unauthorized API access
- Network scanning from containers

### Authentication

- Credentials stored in `~/.maestro/.claude/`
- Mounted read-only in containers
- Each container has isolated state
- GitHub CLI integration is opt-in

### Container Privileges

Containers run with minimal privileges except:
- `NET_ADMIN` capability (required for iptables/firewall)
- Access to Docker socket (not mounted by default)

## Getting Help

- **GitHub Issues**: https://github.com/uprockcom/maestro/issues
- **Documentation**: This guide and the main README.md

## License

Apache 2.0 - See [LICENSE](LICENSE) file for details.
