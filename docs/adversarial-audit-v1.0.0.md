# Cooler Compiler v1.0.0 adversarial audit

- Audit record version: 1.0.0
- Candidate base: `7a793d3d61a8b407916be9d63dd826262e63c907`
- Completion commit: recorded in issue #57's closing comment after exact-commit validation.

This is the retained checklist and evidence record for the final pre-release
correctness, security, and lifecycle audit. Previous issue closure was treated
as a lead only; results below were checked against the integrated candidate.

## Area checklist

| Area | Result | Evidence |
| --- | --- | --- |
| COOL semantics and pipeline | Passed | Conformance, strict differential, property tests, malformed/deep-input tests, and source/report contract tests listed below. |
| IR, optimizer, verifier, and VM | Passed | Optimizer equivalence/property tests, verifier tests, VM policy/transaction tests, fuzzing, race tests, and benchmark smoke. |
| Debugger, heap, and reverse execution | Passed | Debugger lifecycle/concurrency, heap-cycle/truncation, replay/hash, timeline revision, churn, race, and fuzz tests. |
| Local server and sessions | Passed after ADV-001 | Browser launcher reaping regression tests plus HTTP security, session stress, cancellation, bounds, serialization, and shutdown tests. |
| LSP and public API | Passed after ADV-002 through ADV-007 | Framing, request/workspace bounds, shutdown, URI, UTF-16, synchronization, cancellation, public detachment/concurrency, race, and focused platform tests. |

## Confirmed findings

| ID | Severity | Reproduction | Correction and retained regression |
| --- | --- | --- | --- |
| ADV-001 | Major lifecycle | `openBrowser` called `Cmd.Start` without ever calling `Wait`, retaining OS/Go process resources and allowing Unix zombies. | Browser helpers are now time-bounded, asynchronously waited, and killed on timeout. `TestStartAndReap*` covers success, start failure, timeout, kill, and reap. |
| ADV-002 | Major resource exhaustion | LSP framing used `ReadString`, which accumulated an arbitrarily long header line before applying the 16 KiB check; duplicate lengths were ambiguous. | Header fragments are checked while read, over-limit framing is fatal, and duplicate `Content-Length` is rejected. `TestHeaderLimitStopsBeforeReadingUnboundedLine` and `TestDuplicateContentLengthIsRejected` retain the reproductions. |
| ADV-003 | Major unbounded growth/correctness | Each LSP request created a goroutine/map entry without a concurrency ceiling; request and cancellation IDs could each retain message-sized JSON. An invalid `null` request ID was mistaken for an absent ID, allowing a request-shaped `exit` to terminate the session as a notification. | Concurrent requests default to 64, overflow receives a bounded server error, IDs are limited to 256 encoded bytes and LSP string/integer types, duplicate active IDs are rejected, and late cancellations remain count-bounded. Invalid-ID, null-ID exit, and request/cancellation tests retain these bounds. |
| ADV-004 | Major lifecycle | `shutdown` left debounce timers/analysis alive and accepted later notifications, permitting diagnostics and document mutation after shutdown. Document/configuration notifications were also accepted before initialization, and repeated `initialized` notifications retained additional debounce callbacks. | Shutdown invalidates/cancels background work, drains earlier work before responding, and ignores post-shutdown notifications except `exit`; pre-initialize document/configuration notifications and duplicate `initialized` notifications are ignored. Focused lifecycle tests preserve all invariants. |
| ADV-005 | Correctness/security | File-URI conversion discarded non-empty authorities, corrupting Windows UNC identity and mapping remote authorities to local Unix paths. | Windows UNC hosts now round-trip canonically; Unix rejects remote authorities; local paths preserve spaces, Unicode, percent signs, and separators. Focused URI tests cover both platform contracts. |
| ADV-006 | Major lifecycle/resource exhaustion | LSP initialization bounded retained COOL files and bytes but not workspace-root count or filesystem entries visited, and its initial walk had no cancellation deadline. A root over a large tree could retain the protocol loop for an attacker-controlled duration. | Initialization now accepts at most 32 unique roots, visits at most 100,000 entries, inherits request cancellation, and has a five-second scan deadline. Limit/cancellation tests and an invalid-initialize/retry lifecycle test retain the behavior. |
| ADV-007 | Major resource exhaustion | Workspace discovery/reload/watched-file paths trusted a prior `stat` or checked size only after `os.ReadFile`, allowing transient over-limit allocation if a file grew; non-regular paths could also block a read. Repeated open/watched batches could retain far more than the nominal combined workspace ceiling, and one incremental notification could drive quadratic string rebuilding with an excessive change list. | Disk reads reject non-regular paths and stop at the effective byte ceiling; all retained document mutations preserve the combined source ceiling; watched batches collapse duplicate URIs and cap unique files plus combined bytes; incremental notifications accept at most 4,096 changes before requiring full-text resynchronization. Bounded-reader/type, retained-byte, watched-batch, and change-count regressions retain the limits. |

## Rejected leads

| Lead | Reason and supporting invariant |
| --- | --- |
| Embedded web-template panic | The panic is reached only if the process-embedded, build-time-owned template is invalid. It is not influenced by source, input, HTTP, or LSP data; the asset/render tests validate the embedded invariant. |
| Benchmark-workload panic | The panic belongs to generated benchmark setup under repository-owned fixtures, not a public/runtime input path. Benchmark smoke and workload tests validate its construction. |
| Session active-count panic | Lease/pin release is idempotent and serialized under the store lock; stress, expiry, shutdown, and race tests exercise the invariant. The outer HTTP recovery boundary also prevents a host-process panic from escaping a request. |
| Compiler/public API nil context | Public entry points normalize nil contexts through the tested context-error path, and cancellation/concurrency tests cover live contexts. |
| Parser/lexer diagnostic or recursion growth | Parser nesting is capped at 512 and both lexer/parser diagnostics are capped at 100. Malformed and deep generated inputs exercise progress and containment. |
| Deep inheritance recursion | Class traversal is iterative and the retained 6,000-class inheritance tests complete without host-stack growth. |
| Source/project total bytes in the Go API | Callers already own the provided immutable strings; compilation work is linear and cancellation-aware. HTTP and LSP boundaries separately enforce byte/file/document ceilings. No retained internal collection grows independently of caller-owned input. |

## Validation record

The completion commit is validated from a clean checkout. Exact command results,
tool versions, platform details, hashes, and durations are recorded in issue
#57's closing comment. The required gate is:

```text
make conformance
STRICT=1 make differential
make lint
make property-test
make session-stress
make fuzz TARGET=all DURATION=30m
CGO_ENABLED=1 go test -race -count=10 ./...
make benchmark-smoke
make release-check
```

Focused retained coverage additionally includes browser subprocess start/failure/
timeout/reaping, LSP header and request limits, shutdown ordering, rapid document
updates, file URI round trips with spaces/Unicode/percent escapes, Windows UNC
authorities, Unix remote-authority rejection, UTF-16 boundaries, failing writers,
closed pipes, and public API/debugger cancellation and concurrency.

## Diff-only final review

The audit-fix diff is reviewed independently after all focused tests pass and
before the exact-commit gate. Any actionable finding discovered there is added
to the confirmed table, fixed, and retested before closure.
