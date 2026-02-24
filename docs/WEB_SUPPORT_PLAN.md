# Plan: Add Browser/Web Rendering Support to Maestro Containers

## Context

Agents working on JS/web projects inside Maestro containers have no way to see how their pages render in a real browser. We're adding a **pre-baked Docker image variant** (`maestro-web`) with headless Chromium + Playwright MCP, so the Claude agent inside the container gets 34 browser automation tools (screenshot, navigate, click, evaluate JS, inspect accessibility tree, etc.) — all running headless with no display server required.

**Key technical insight**: Playwright (since v1.49) ships its own `chromium-headless-shell` binary that needs zero X11/Xvfb/Wayland/D-Bus. It only needs ~20 X11 client libraries (shared `.so` files, not a server). The MCP server runs as a stdio subprocess launched on-demand by Claude Code.

## Image naming

- Separate image: `ghcr.io/uprockcom/maestro-web:latest` / `ghcr.io/uprockcom/maestro-web:{version}`
- Built FROM the base maestro image (layered ~250MB delta)

## Changes by File

### Phase 1: Docker Image and Make targets
1. **`docker/Dockerfile.web` (NEW)**
   - FROM `maestro:latest`
   - Install Playwright system dependencies (fonts-liberation, Chromium X11 client libs)
   - Install `@playwright/mcp` globally
   - Run `npx playwright install chromium`
   - `ENV MAESTRO_WEB=true`

2. **`Makefile` (MODIFY)**
   - Add `docker-web:` target (depends on `docker`) building from `docker/Dockerfile.web` with `BASE_IMAGE` build-arg.
   - Update `all:` to include `docker-web`.

### Phase 2: Configuration and Core Structs
1. **`cmd/root.go` (MODIFY)**
   - Add `Web` section to `Config` struct.
   - Add defaults (`web.enabled`, `web.image`, `web.shm_size`) to `initConfig()`.

2. **`pkg/version/version.go` (MODIFY)**
   - Add `GetContainerWebImage()` returning `ghcr.io/uprockcom/maestro-web:<version>`.

3. **`pkg/container/types.go` (MODIFY)**
   - Add `HasWeb bool` to `Info` struct.

4. **`pkg/api/containers.go` (MODIFY)**
   - Add `HasWeb bool` to `ContainerInfo` struct so it can be passed over the new typed HTTP API.

5. **`pkg/containerservice/service.go` (MODIFY)**
   - Update `toContainerInfoSlice` to map the `HasWeb` field from the API struct to the internal `container.Info` struct.

### Phase 3: Core Container Creation
1. **`cmd/new.go` (MODIFY)**
   - Add `--web` boolean flag.
   - Add `WebEnabled bool` to `ContainerSetupOptions`.
   - Add `getDockerWebImage()` to return web image name (from config or version).
   - In `runNew`, pass resolved `WebEnabled` to `setupContainer`.
   - In `setupContainer`, determine image name based on `opts.WebEnabled` and pass it to `startContainerWithLabels`.
   - Pass `opts.WebEnabled` to `writeClaudeSettings` (to inject MCP) and `writeMaestroMD` (for docs).
   - Modify `startContainerWithLabels` signature to accept `webEnabled`. Add `--label maestro.web=true`, `--shm-size`, and `--init` flags when true. Select correct image.
   - Inject `mcpServers.playwright` config in `writeClaudeSettings` when web is enabled.

2. **`assets/MAESTRO.md` (MODIFY)**
   - Add `{{WEB_TOOLS_SECTION}}` placeholder for browser tool docs. Modify `writeMaestroMD` to inject docs here if web enabled.

### Phase 4: TUI and UI Indicators
1. **`pkg/tui/messages.go` & `pkg/tui/model.go` (MODIFY)**
   - Add `Web bool` to `createContainerMsg` and `TUIResult`.
   - Add "Enable browser support (--web)" checkbox to create container modal.

2. **`pkg/container/info.go` (MODIFY)**
   - Parse the `maestro.web` label during `GetAllContainers`/`GetRunningContainers` and set `info.HasWeb`.

3. **`pkg/container/display.go` (MODIFY)**
   - Update table rendering to append `/web` to the status if `HasWeb` is true.

### Phase 5: Daemon IPC Chain
1. **`docker/maestro-request-go/cmd_new.go` & `request.go` (MODIFY)**
   - Add `--web` flag to `maestro-request new` and pass `web: true` in the IPC JSON payload.

2. **`pkg/daemon/ipc_types.go` & `ipc.go` (MODIFY)**
   - Add `Web bool` to `IPCRequest` and handle it in `handleNewContainer`, passing it to `createChildContainer`.

3. **`pkg/daemon/daemon.go` (MODIFY)**
   - Add `WebEnabled bool` to `CreateContainerOpts` struct.

4. **`cmd/new.go` (MODIFY)**
   - Update `createContainerFromDaemonOpts` to pass `opts.WebEnabled` into `CreateContainerFromDaemon`.
   - Update `CreateContainerFromDaemon` to accept `webEnabled bool`. If false, inspect parent container for `maestro.web=true` label. Pass resolved value in `ContainerSetupOptions` to `setupContainer`.

## Verification
- Build: `make docker && make docker-web`
- Create web container: `maestro new --web "test"`
- Ensure Settings has playwright MCP server.
- List display shows `running/web`.
- Inherits correctly through `maestro-request new`.
