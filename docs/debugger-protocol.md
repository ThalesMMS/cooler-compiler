# Debugger and session protocol

This is the current protocol between the embedded web client and Cooler's local
Go process. It is documented for maintenance and local tooling, not as a hosted
public API. Complete result objects follow the
[versioned artifact contract](artifact-contract.md); endpoint compatibility is
otherwise limited to the same application release.

## Transport and authorization

The server defaults to an ephemeral loopback port. State-changing endpoints are
JSON `POST` requests and require the `X-Cooler-Capability` value injected into
the initial HTML. Read-only `/api/health` and `/api/examples` are `GET` requests.
Responses are JSON and `no-store`. The [security policy](../SECURITY.md#local-server-threat-model)
defines the trust boundary and why non-loopback mode is not a deployment model.

Request bodies are limited to 1 MiB and reject unknown JSON fields or trailing
values. At most 32 sessions are retained; an idle session expires after 30
minutes. Each session is locked while one action runs, so step/run/reset and
pipeline transitions are serialized.

The formal state machine, valid-operation table, TTL/LRU rules, atomic
promotion, disposal semantics, and local metrics are defined in
[`session-lifecycle.md`](session-lifecycle.md).

## Compilation lifecycle

`POST /api/pipeline/start` accepts a compilation request, creates a session,
executes Lexer, and returns `{ "id", "snapshot" }`. A request contains either
`"source": string` or ordered `"sources": [{ "id": string, "name": string,
"content": string }]`, plus optional `"input": string`, `"vmPolicy": object,
`"optimization": "none" | "basic"`, and `"interactive": boolean`. When
`sources` is present it takes precedence over `source`. The optimization mode
is fixed for that session and defaults to `none`; `interactive` is preserved if
the pipeline session is later promoted to a VM session.

- `/api/pipeline/next` accepts `{ "id", "input"? }` and performs exactly one
  remaining phase.
- `/api/pipeline/complete` accepts the same shape and performs every unblocked
  phase without executing a VM instruction.
- `/api/pipeline/dispose` accepts `{ "id" }` and removes the session.

The ordered phases and blocked/error behavior are defined in
[`internal/compiler/pipeline.go`](../internal/compiler/pipeline.go). An expired,
disposed, or unknown ID returns HTTP 404.

`POST /api/compile` is a stateless convenience endpoint accepting source/input,
optional `vmPolicy`, `optimization`, and `maxSteps`, and returning a fully
compiled and executed report. `maxSteps` defaults to 100,000 and cannot exceed
the selected policy's per-run bound. Interactive execution is rejected because
the endpoint does not create the reusable session needed for continuation.

## VM lifecycle

`POST /api/vm/start` accepts source plus optional input, `interactive`, and
`vmPolicy`, and `optimization`, completes compilation, creates a VM, and returns
a full debugger snapshot. Pipeline start accepts the same interactive flag and
preserves it on later VM promotion. A policy and optimization mode may only be
selected when the session starts. `none` remains the debugger default because
optimized instruction stepping can differ even when source behavior does not.

The action request is:

```json
{
  "id": "opaque-session-id",
  "input": "optional replacement input\n",
  "maxSteps": 1000,
	"breakpoints": [{
	  "id": "main:8",
	  "sourceId": "main",
	  "line": 8,
	  "column": 5,
	  "condition": "i = 10",
	  "hitCount": 5,
	  "logMessage": "i={i}",
	  "pause": false,
	  "enabled": true
	}],
	"watches": [{ "id": "watch-1", "expression": "i + 1", "enabled": true }],
  "light": true,
  "traceFrom": 120
}
```

- `/api/vm/step` executes at most one instruction.
- `/api/vm/step-into`, `/api/vm/step-over`, and `/api/vm/step-out` execute
  until their next user-visible source condition. Requests accept the same
  bounded `maxSteps` and source-aware `breakpoints` as Run.
- `/api/vm/run-to-cursor` additionally requires
  `{ "cursor": { "sourceId", "line", "column"? } }`. The server resolves the
  position against verified executable instructions for that source unit. A
  location with no executable instruction returns 409 without mutating the VM;
  the temporary target exists only for that serialized operation.
- `/api/vm/run` executes up to `maxSteps`, halting, erroring, or pausing before
  a matching 1-based source line. Valid requests use 1–1,000,000 steps and at
  most 10,000 positive breakpoint lines; the session policy may impose a lower
  bound and clamps a run chunk to its `maxInstructionsPerRun`.
- `/api/vm/reset` recreates the VM from the compiled executable while preserving
  the configured pre-supplied input, discarding live submissions/history, and
  returns an unexecuted snapshot.
- `/api/vm/input` accepts `{ "id", "line", "inputSequence", "light"? }`
  only in `waiting-input`. The line is at most 64 KiB and remains subject to the
  session VM policy. A valid request executes exactly the pending input opcode;
  wrong sequences, duplicate delivery, and other session states return 409.
- `/api/vm/heap` is a capability-protected, read-only query accepting
  `{ "id", "heap": { ... } }`. It is valid while the VM is stopped in
  `ready`, `paused`, `waiting-input`, `halted`, or a resettable `failed` state.
  It neither changes lifecycle state nor executes an instruction.
- `/api/vm/dispose` removes the shared pipeline/VM session.

Pre-supplied input may be set until or during a non-input session request;
reset rewinds its line cursor. Once waiting, only input, reset, or disposal is
valid. The browser disables Step/Run, focuses a labelled live-input field,
renders submitted lines separately from program output, and guards both session
ID and client epoch so an edited/replaced source cannot receive a stale reply.
Channel selection, EOF, terminal behavior, line values, and replay are defined
by the cross-surface [program input contract](input.md).

Every debugger snapshot also carries `location` and `stopReason`. A location is
the identity of the next instruction: source-unit ID, full span, method,
method-local instruction index, zero-based frame depth, and `synthetic` flag.
Stop reasons are `ready`, `instruction-step`, `step-into`, `step-over`,
`step-out`, `run-to-cursor`, `breakpoint`, `waiting-input`,
`instruction-limit`, `runtime-error`, `halt`, or `cancelled`.
Reverse responses additionally use `reverse-step` and `reverse-continue`.

Source stepping compares full spans, methods, and frame depth rather than line
numbers. Step Into stops at the next distinct visible construct and enters user
methods and user-authored attribute initializers; built-ins remain atomic. Step
Over ignores locations in deeper frames. Step Out waits until the current frame
has returned; from the synthetic entry frame it runs to another terminal stop.
Generated entry/initializer scaffolding is marked synthetic and skipped, while
its raw instruction and source mapping remain available through Step
Instruction and the trace. At a shared boundary the precedence is user
breakpoint, requested source condition, runtime/input/halt, then instruction
chunk limit. Breakpoint skip state guarantees that resume executes an
instruction before the same pre-execution breakpoint can stop again.

### Conditions, hit counts, logpoints, and watches

Legacy integer and `{sourceId,line}` breakpoints remain accepted. Enriched
breakpoints add a stable client ID, optional column, condition, hit threshold,
log message, pause-after-log flag, and enabled state. The VM verifies each
location and returns verification, diagnostic, and encounter count. A column
selects the most-specific executable span containing it, avoiding duplicate
source-location hits from nested instructions on one line.

An encounter is counted immediately before its instruction and before condition
evaluation. A condition runs on every encounter and must produce `Bool`; it
triggers only when true and the raw encounter count is at least `hitCount` (`0`
means no threshold). False, invalid, and non-Boolean conditions always allow
execution to progress. Their diagnostics belong to the breakpoint and never
become COOL runtime errors. Counts persist across requests in one VM; Reset and
recompilation create a fresh VM and reset them.

Debugger expressions reuse COOL tokenization, parsing, precedence, Int32
arithmetic, equality, and display. The side-effect-free subset is
local/formal/attribute lookup, `self`, `void`, Int/String/Bool literals,
parentheses, `+ - * / < <= =`, `not`, `~`, and `isvoid`. Assignment, allocation,
dispatch (including built-ins), control flow, blocks, `let`, and `case` are
rejected. Evaluation reads a stopped frame and object fields directly with a
256-node/1,024-byte bound; it never executes bytecode or mutates stack, frames,
output, or heap.

A non-empty `logMessage` makes a logpoint. Braced expressions are interpolated,
for example `value={value}`. Logpoints do not pause unless `pause:true` and
append to `debugLog`, never program stdout. Messages allow 1,024 bytes and 32
interpolations; the session retains at most 256 entries/64 KiB. Entries use a
deterministic order counter, source, line, method, and VM step instead of wall
time.

At most 64 watches are accepted. They evaluate only in stopped snapshots and
are cached by ID/expression/step/frame depth. Each result contains a compact
cycle-safe value, static/dynamic type, dependencies, source context, or an
isolated diagnostic. A failing watch cannot fail the request or program.

### Bounded heap queries

Heap data is queried on demand and is never embedded in full or light debugger
snapshots. Query `mode` is one of `summary`, `roots`, `object`, `graph`, or
`search` (an omitted mode means `graph`). `object` and a centered `graph`
accept a stable ID such as `Node#2`; `search` accepts exact `class`, an optional
ID substring, and `offset`. A truncated search returns `nextOffset`.

```json
{
  "id": "opaque-session-id",
  "heap": {
    "mode": "graph",
    "id": "Node#2",
    "limits": {
      "maxObjects": 100,
      "maxFieldsPerObject": 64,
      "maxDepth": 4,
      "stringPreview": 128,
      "maxBytes": 262144
    }
  }
}
```

Zero limit values select those defaults. Server maxima are respectively 1,000
objects, 1,000 fields per object, depth 32, 4,096 Unicode code points, and 2
MiB; `maxBytes` has a 1 KiB minimum. Invalid limits are HTTP 400 and an unknown
object is HTTP 404. The response reports normalized limits, actual serialized
bytes, total allocation count, debugger-root-reachable count, objects, roots,
truncation, and continuation where applicable.

Object IDs contain dynamic class plus monotonic allocation order, never an
address. Fields retain declaring owner, static type, and source span in
deterministic inheritance/source order. Values explicitly identify void,
primitive, string preview, or reference; a reference omitted by a graph/byte
bound is marked `unavailable`, not disguised as void. Traversal is iterative
and cycle-safe. Reachability means only traversal from debugger roots (`self`,
lexical locals/formals, and operand stack); it is not a claim about Go garbage
collection or leaked memory.

The Heap tab groups roots by frame and stack, exposes each object as an
expandable field tree/table, and uses native buttons for reference navigation.
Runtime stack/frame and watch references open the same bounded graph. Field
rows navigate to their declaring source span. The table/list is the accessible
non-graph representation and all inspection remains keyboard-operable.

### Deterministic reverse timeline

Every verified debugger VM owns a bounded reverse timeline. Mutable state is
deep-cloned cycle-safely into checkpoints while the verified executable and
parsed debugger expressions remain shared immutable data. A checkpoint is
taken at step zero, every 32 executed instructions, and at meaningful stops
(source commands, breakpoints, input waits, instruction limits, errors,
cancellation, and halt). It contains frames/IPs/scopes, stack, object graph and
next ID, input/output, trace retention state, diagnostics, debugger hit/log
state, status, and normalized VM policy.

Between checkpoints the session retains lightweight ordered execution markers,
breakpoint configuration boundaries, accepted live-input lines, and explicit
interactive-EOF completion. Live lines
are sensitive in-memory replay data and are not returned by timeline snapshots.
The public `executableHash` identifies compiler/schema, source-unit digests, and
VM policy; `stateHash` canonically covers execution, frames, scopes, stack,
objects, IDs, input/output, diagnostics, breakpoint hits, logpoints, and policy.
Neither hash uses timestamps, Go addresses, or map iteration order.

Default limits are 64 checkpoints/16 MiB and 8,192 events/2 MiB. The checkpoint
interval and all bounds are normalized by `ReverseHistoryLimits`; maxima are
1,024 checkpoints/256 MiB and 1,000,000 events/64 MiB. Old history is evicted
deterministically, and `earliestReversibleStep` advances explicitly. Immutable
compiler artifacts are not duplicated. If the event budget is exhausted, the
current VM becomes the new earliest checkpoint rather than allowing an
unbounded log.

`POST /api/vm/step-back` requires `{ "id", "timelineRevision",
"reverseMode": "instruction"|"source" }`. Instruction mode restores the
previous opcode boundary. Source mode restores the preceding recorded source
stop (or the earliest retained point). `POST /api/vm/reverse-continue` requires
`{ "id", "timelineRevision" }` and returns to the preceding breakpoint,
source stop, input request, instruction-limit/error/halt boundary, or start.
Both accept light snapshots, `traceFrom`, and watches, and are valid while the
VM is stopped in ready, paused, waiting-input, halted, or resettable failed
states.

`POST /api/vm/seek` requires `{ "id", "timelineRevision", "targetStep": N }`
and accepts the same light/trace/watch options. It selects any retained step
between `earliestReversibleStep` and `latestStep`, in either direction, without
truncating checkpoints, events or recorded input. Invalid/missing steps return
400; stale revisions and unavailable/evicted targets return 409. The public
`DebugSession.SeekRetainedStep(ctx, step, revision)` and optional
`HistoryNavigator` interface expose the same operation, with
`ErrTimelineRevision` and `ErrReverseUnavailable` identities.

The result uses `history-seek`. At a step with multiple non-instruction events,
the latest retained checkpoint at that step wins; this is instruction navigation,
not selection of each individual input/breakpoint event. Replay consumes saved
input automatically and is bounded by the retained checkpoint interval (hard
maximum 10,000 instructions). Cancellation is checked before cloning, during
replay and before publication; failed/cancelled replay leaves the live VM and
history position unchanged. Unlike forward execution cancellation, it does not
insert a runtime diagnostic into the selected program.

Restoration clones the nearest checkpoint and replays forward at most the
retained interval. Breakpoint conditions/hit counts and logpoint state are
re-evaluated deterministically; UI log rendering itself is not an execution
input. Recorded live input is consumed without prompting until replay reaches a
new input boundary. The restored result uses stop reasons `reverse-step` or
`reverse-continue` and synchronizes source, frames, stack, heap, watches, and
output. Trace state begins with the checkpoint's bounded retained trace and the
replayed instructions; absolute timeline position remains in `timeline`.

Timeline revision is mandatory on reverse requests. A stale revision returns
HTTP 409. After a reverse operation, any forward instruction/source/run/input
action explicitly removes checkpoints, events, inputs, and stops in the old
future, increments the revision, and reports `futureDiscarded`. Reset starts a
new revision at step zero and does not silently preserve the old future.

The GUI provides a labelled instruction/source mode selector, native Back and
Reverse buttons, disabled states derived from `canStepBack` and
`canReverseContinue`, and an `aria-live` timeline summary containing current,
earliest, latest, and revision. When current is behind latest it labels console
output as historical; a branch announces future discard. Existing client epoch
checks and the server timeline revision jointly prevent stale responses.

The GUI also provides a native range slider and numeric step field with keyboard
support. Drags coalesce for 80 ms with at most one request in flight and one latest
destination queued. A request sequence rejects older drag responses even within
the same revision. Historical input display is filtered without deleting the
saved entries, so returning to the retained future restores them. Actual forward
execution prunes the obsolete future. This remains a bounded, linear timeline,
not a persistent tree of branches.
