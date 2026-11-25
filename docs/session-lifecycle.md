# Session lifecycle and concurrency contract

The local GUI server owns bounded compilation/debug sessions. This contract
defines their state machine, locking, expiry, and failure behavior. It applies
within one Cooler process; session IDs are not durable across restarts.

## States

| State | Meaning |
| --- | --- |
| `created` | Pipeline exists but has not advanced |
| `compiling` | A phase is executing or more phases remain |
| `ready` | Verified debugger VM is stopped and accepts execution |
| `running` | A serialized VM operation currently owns the session |
| `paused` | A run chunk or breakpoint paused execution |
| `waiting-input` | VM is stopped before an input opcode and awaits one numbered line |
| `halted` | Program completed normally; reset is available |
| `failed` | Compilation, runtime, cancellation, or recovered host failure is terminal; runtime failures may reset |
| `disposed` | Explicitly removed |
| `evicted` | Removed to make capacity for a newer session |
| `expired` | Removed after the idle TTL |

Disposed, evicted, and expired entries are tombstone states only on references
already held by local tests or an in-flight operation. They are removed from the
store immediately and cannot be looked up or resurrected.

## Valid operations

| Operation | Valid source states | Result |
| --- | --- | --- |
| Pipeline `next` | `created`, `compiling` | Advances exactly one phase |
| Pipeline `complete` | `created`, `compiling` | Advances through the terminal compilation phase |
| VM `step` / `run` | `created`, `compiling`, `ready`, `paused` | Atomically promotes an unfinished pipeline, then executes |
| VM source step / run-to-cursor | `created`, `compiling`, `ready`, `paused` | Executes one bounded source-level command; cursor targets are operation-local |
| VM `input` | `waiting-input` | Accepts one matching request sequence and executes exactly its pending opcode |
| VM `reset` | `ready`, `paused`, `waiting-input`, `halted`, resettable runtime `failed` | Rebuilds the VM without changing its session ID |
| VM `heap` | `ready`, `paused`, `waiting-input`, `halted`, resettable runtime `failed` | Returns a bounded read-only heap projection without changing state |
| VM `step-back` / `reverse-continue` / `seek` | `ready`, `paused`, `waiting-input`, `halted`, resettable runtime `failed` | Restores a retained timeline state; requires the current revision |
| Dispose | Any retained state | Removes the ID immediately; reports whether it existed |

Compilation failures are not resettable because the verified executable does
not exist. Optional pre-supplied input on a valid serialized VM/pipeline request
is applied immediately before that request's action. While waiting, step/run
and replacement input are invalid; only a numbered live-input delivery, reset,
or disposal may proceed. The normal per-session mutex serializes competing
deliveries, so only the first matching request can succeed.

Malformed requests and missing IDs return HTTP 400. For pipeline/VM actions, a
syntactically valid but unknown, disposed, evicted, or expired ID returns HTTP
404. Dispose itself is idempotent HTTP 200 with `disposed:false` when the ID is
already absent. A known session in an invalid state returns HTTP 409 with stable
fields `error`, `operation`, and `state`. Creating a session while every
capacity slot is actively owned returns HTTP 503. Unexpected request failures
return HTTP 500 and never continue a corrupted pipeline; VM panic recovery
leaves an inspectable failed snapshot.

## Serialization and removal

An operation first acquires a short store-level lease, then the session mutex.
The global store mutex is never held while compiler or VM work runs. Therefore
same-session actions never overlap while unrelated sessions can progress
concurrently. Concurrent callers must not depend on scheduler arrival order.

Heap inspection acquires the same lease and mutex so it observes a coherent
stopped VM, but it never sets `running`, applies input/watches, updates session
state, or promotes an unfinished pipeline. Its capability requirement protects
program values even though the operation itself is read-only.

Reverse operations use the same serialized lease and temporarily enter
`running` while a checkpoint is restored/replayed, then derive the normal
stopped state from the returned snapshot. They never overlap a forward command.
The request's `timelineRevision` is checked while holding the session mutex;
stale revisions return HTTP 409. A forward operation from a historical position
atomically truncates the old future and increments the revision before it
executes. Reset deliberately starts another revision at step zero.

The active-operation count is incremented before waiting for the session mutex.
TTL and LRU pruning skip every entry with an active or queued lease. Dispose
removes the ID immediately; operations that acquired a lease earlier may finish,
but later lookups receive 404. Pipeline/VM pointers, input, and other retained
state are cleared after the final lease releases. Response encoding happens
after lease release, so a disconnected client or failing response writer cannot
retain the lock.

A newly registered session carries a lock-free active pin until its creation
response finishes. This prevents capacity pruning from returning an ID that was
already evicted; the pin is released even when response writing fails.

Idle TTL is measured from operation completion. Capacity pruning removes
expired idle sessions first, then the least-recently-used idle session. Equal
timestamps are resolved by lexicographically smallest session ID, independent
of Go map iteration. When all slots are active, creation is rejected instead of
evicting live work or exceeding the bound.

Server shutdown stops accepting work, waits for active HTTP handlers, and then
closes the store. Closing removes all IDs immediately and applies the same
deferred cleanup rule to any operation that already owns a lease.

## Identity, stale responses, and metrics

Production IDs contain 128 random bits encoded as 32 lowercase hexadecimal
characters. Generation is injectable in tests; collisions are retried without
replacing or evicting the existing session. Requests strictly validate the
shape before lookup. The process capability remains the authorization boundary.

The browser pairs the server ID with a monotonically increasing client epoch.
Editing/replacing a source invalidates the epoch; old responses cannot update
new state, and disposal is deferred while an old Run request is still in flight.

`sessionStore.snapshot()` is a local test/debug view containing only active
session/operation counts, state distribution, evictions, expirations, and
rejected operations. It never contains source, input, output, tokens, or VM
values.

Run the bounded race-enabled stress suite with:

```bash
make session-stress
```

`SESSION_STRESS_COUNT` (1–1000) and `SESSION_STRESS_TIMEOUT` tune the local run.
The release gate repeats the deterministic non-race lifecycle suite. No hosted
CI is required.
