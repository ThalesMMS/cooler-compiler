# Issue #63: browser helper timeout assertion

## Diagnosis and implementation plan

The timeout helper is already killed and collected by `exec.Cmd.Wait`.
`ProcessState.Exited()` rejects signal termination on Unix, so it is not a
portable assertion for this test. Keep the normal-exit and start-failure tests,
the deadline check, and the five-second bound. Require the timeout result to be
an `*exec.ExitError` with a non-nil, unsuccessful process state identical to the
command's state. This verifies that Wait collected the actual child on both
Windows and Unix without prescribing a platform-specific exit code.

## Validation

- Windows, `CGO_ENABLED=0 go test ./cmd/coolergui -run
  '^TestStartAndReapKillsTimedOutBrowserCommand$' -count=3`: passed before and
  after the change.
- Linux executable built using `CGO_ENABLED=0 GOOS=linux go test -c` and run on
  Debian/WSL with `-test.run=^TestStartAndReapKillsTimedOutBrowserCommand$
  -test.count=3`: all three runs failed before the change with `signal: killed`;
  all three passed after it. This is an actual Linux execution, using the test
  binary because the WSL distribution has no Go installation.
- Complete `cmd/coolergui` suite: passed on Windows and Debian/WSL.
- Temporary mutations removing Wait or dropping its result: the timeout test
  failed in both cases. Both mutations were restored, and all three
  `TestStartAndReap` tests then passed three times on Windows.
- `CGO_ENABLED=0 go test ./...`, `go vet ./...`, and
  `go run ./cmd/cooler-doc-check`: passed.
- macOS runtime validation was unavailable on this Windows host. Unix signal
  termination was reproduced and validated on Debian/WSL.

No production behavior changes are included.
