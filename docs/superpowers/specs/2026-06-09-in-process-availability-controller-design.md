# In-Process Availability Controller Design

## Goal

Implement the first version of the availability state writer inside the backend process.
Every backend pod starts the same controller loop, but only the pod that holds the shared
state-file lock may publish decisions to `/availability/state.json`.

The goal is to remove the current manual/external writer gap and keep one simple,
fresh availability state for all pods.

## Current State

The backend already has:

- `availability.State` validation, stale detection, and monotonic epoch checks.
- `availability.FileStore.Write`, which writes with a `.lock` file, temp file, fsync, and atomic rename.
- `availability.Watcher`, which reads the state file and exposes a snapshot.
- active Redis selection for item, WebSocket ticket, presence, and event bus paths.
- bid-path rejection when the control-plane state is invalid, protected, or past the MySQL buffering window.

Missing piece:

- no production controller currently writes or refreshes the state file.

## First-Version Architecture

Each backend pod runs two availability loops:

- watcher loop: existing read-side loop, refreshed at half of `stale_threshold`.
- controller loop: new write-side loop, started after cloud Redis, local Redis, and MySQL clients are created.

Only one controller may publish at a time. The controller attempts to acquire the same file lock used by
`FileStore.Write`. If it cannot acquire the lock promptly, it skips that tick and keeps serving as a reader.

This gives shared leadership without assigning a fixed leader pod. If the current writer pod dies, another pod
can acquire the lock on a later tick.

This first version intentionally does not model per-pod network partitions. It assumes all backend pods have
the same Redis reachability: either they can all reach cloud Redis, or they all cannot. The file lock is for
single-writer simplicity and handoff.

## Inputs

The controller probes:

- cloud Redis: `PING` against `cfg.Redis`.
- local Redis: `PING` against `cfg.Availability.LocalRedis`, falling back to `cfg.Redis` if local Redis is not configured.
- MySQL: `PingContext` through the existing `gorm.DB` SQL pool.
- previous state: the current file if it exists and validates enough to preserve epoch.

Probe timeouts should be short and bounded. The controller must not let a slow dependency block server shutdown.

## State Decisions

The first version should keep the state machine intentionally coarse.

Normal:

- If cloud Redis and MySQL are healthy, write `mode=normal_cloud`, `active_redis=cloud`, `mysql_state=healthy`.
- Keep the previous epoch unless returning from a different Redis authority or a protected/switching state.

Cloud Redis failure:

- If cloud Redis fails for at least `redis_failover_threshold` and local Redis is healthy, switch to local authority.
- Increment epoch when changing `active_redis` from `cloud` to `local`.
- Write `mode=local_redis_active`, `active_redis=local`.
- If local Redis is also unhealthy, write `mode=auction_protected` and do not point writes at an unusable authority.

MySQL failure:

- If Redis authority is healthy and MySQL becomes unavailable, write `mode=mysql_buffering`, `mysql_state=buffering`, and set `mysql_buffering_started_at_unix_ms`.
- Preserve the buffering start timestamp while MySQL remains unavailable.
- Once `mysql_buffering_window` expires, keep publishing a fresh state but rely on bid service enforcement to reject writes.
- Settlement remains paused while MySQL is down or buffering.

Recovery:

- When MySQL recovers, clear buffering fields and return to the Redis mode implied by Redis probes.
- When cloud Redis recovers after local authority, switch back to `normal_cloud` and increment epoch so stale local-era writes are rejected.
- Existing item-level reconcile logic moves active auction state from local Redis back to cloud Redis on demand.

Control-plane protection:

- If file state cannot be read safely, or no Redis authority is usable, publish `mode=auction_protected` when the writer can still write the file.
- If the controller cannot write the file, watchers eventually mark it stale and write paths fail closed.

## Epoch Rules

Epoch is the guardrail against stale writes across authority changes.

- Preserve epoch when only refreshing `updated_at_unix_ms` or changing MySQL status within the same Redis authority.
- Increment epoch whenever `active_redis` changes.
- Increment epoch when leaving `auction_protected` for an active authority.
- Never publish an epoch lower than the last valid file epoch.

## File Write Protocol

Use `availability.FileStore.Write` as the single write path.

Required behavior:

- acquire exclusive file lock;
- re-read current state under the lock before deciding final epoch;
- write temp file in the same directory;
- fsync file;
- atomic rename over `state.json`;
- fsync directory when possible;
- release lock.

If `FileStore.Write` does not currently expose a way to combine lock acquisition, re-read, and decide, extend it with a small helper rather than duplicating locking code.

## Startup Behavior

The server should start the controller after Redis clients and DB pool are available.

Initial state handling:

- If the state file is missing, the leader writes an initial state based on probes.
- If the state file is stale but probes are healthy, the leader refreshes it.
- If the first write fails, startup should continue; `/readyz` and bid writes will reflect stale or invalid control-plane state until a later controller tick succeeds.

Do not make Redis availability a hard startup dependency.

## Observability

Add low-cardinality metrics/logs for:

- controller tick result: `leader`, `not_leader`, `write_success`, `write_error`.
- probe results for cloud Redis, local Redis, and MySQL.
- state transition: previous mode/authority/epoch to next mode/authority/epoch.

Do not log DSNs, Redis passwords, Secret values, or full config.

## Testing

Unit tests:

- missing file writes initial healthy state.
- stale file is refreshed by the leader.
- lock contention causes non-leader tick to skip without writing.
- cloud Redis failure beyond threshold switches to local Redis and increments epoch.
- cloud Redis recovery switches back to cloud and increments epoch.
- MySQL failure enters buffering and preserves buffering start timestamp.
- buffering expiration is represented by fresh state while bid service rejects writes.
- no usable Redis authority publishes protected mode.

Integration-style local test:

- run two controller instances against the same temp state path;
- verify only one writes per lock window;
- verify watcher observes mode and epoch changes.

## Non-Goals

- Do not implement Kubernetes Lease in this version.
- Do not implement multi-node consensus.
- Do not continuously mirror cloud Redis state into local Redis.
- Do not make backend startup succeed when MySQL cannot be opened; that is a separate change.
- Do not move the controller into a sidecar yet.

## Open Operational Assumptions

This version assumes the current single-node k3s deployment, where `/var/lib/live-auction/availability`
is shared by all backend pods through hostPath.

It also assumes backend pods observe the same Redis availability. Multi-node deployment or scenarios where
different pods can see different Redis health need a later design using Kubernetes Lease, etcd, or another
consensus-backed store.
