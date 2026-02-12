# Wait Command Roadmap

The `maestro-request wait` command provides blocking primitives for container orchestration. This document tracks planned wait types beyond the initial implementation.

## Implemented

### `wait request <id> <status>`
Blocks until a request file reaches the target status. Core primitive for spawn-and-wait patterns.

### `wait script <command> [args...]`
Runs a command with timeout. General-purpose wrapper for any blocking operation.

### `wait daemon`
Blocks until the daemon IPC channel is reachable. Startup synchronization.

## Planned

### `wait message`
Block until a message is received from the parent or another container. Enables bidirectional communication between containers beyond the current request/response model. Use cases: parent sending configuration updates, coordinating multi-container workflows, passing intermediate results.

### `wait firewall`
Block until a domain is added to the firewall whitelist. When a container needs access to an unlisted domain, it can request approval and wait rather than failing immediately. Use cases: dynamic dependency resolution, containers that discover needed domains at runtime.

### `wait approval`
Block until a human approves an action via the daemon's notification system. Enables human-in-the-loop workflows where certain operations require explicit user consent. Use cases: deployment approvals, destructive operations, cost-sensitive API calls.

### `wait lock <resource>`
Acquire a distributed lock across containers. Prevents concurrent access to shared resources (e.g., a git branch, a deployment target). Use cases: coordinated pushes to the same branch, sequential deployments, shared file access.
