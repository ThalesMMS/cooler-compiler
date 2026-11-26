# Tests for your COOL programs

Run a local exercise without the compiler's internal conformance matrix:

```sh
coolerc test examples/exercise-tests/cases.json
coolerc test --json examples/exercise-tests/cases.json > results.json
```

The [complete example](../examples/exercise-tests/cases.json) contains ordered
multifile programs, independent inputs, expected semantic/runtime errors, missing
input and a bounded loop. Its six cases pass as distributed. Change the program
or an expected result to see a failing case. Run from any working directory:
source names resolve relative to the manifest's directory.

## Manifest 1.0.0

```json
{
  "schemaVersion": "1.0.0",
  "cases": [{
    "name": "increment 41",
    "sources": ["helper.cl", "main.cl"],
    "input": "41\n",
    "limits": {"maxSteps": 1000, "timeoutMs": 2000},
    "expect": {"outcome": "success", "output": "42"}
  }]
}
```

Cases and source files execute in their listed order. Each case gets a fresh
public compiler session and VM, with isolated input, output, heap and history.
No subprocess, network service or standard-input prompt is used.

`expect.output` is mandatory, including the empty string. Output comparison is
exact UTF-8 text: no trimming, case folding, Unicode normalization, newline
conversion or implicit final newline. Text failures show the first differing
zero-based byte offset and escaped expected/actual strings, so spaces, `\n`
and `\r` remain visible. JSON escapes are decoded as ordinary JSON strings.
The VM's existing input semantics still apply: CRLF separates input lines like
LF and a final line terminator does not add another line. An empty input string
provides no lines; `"\n"` provides one empty line.

| Expected outcome | Meaning |
| --- | --- |
| `success` | VM halts normally with no error diagnostics |
| `error` | Compiler or VM reports errors; list their exact ordered codes and stages |
| `input-required` | Preloaded input is exhausted and VM suspends; no fabricated EOF value |
| `step-limit` | VM reaches the case's instruction budget without halting or waiting for input |

For errors, add `"diagnostics": [{"code":"COL6015","stage":"runtime"}]`
inside `expect`. Warnings do not participate in the error comparison. Matching
is exact over the entire ordered error list, not merely the presence of one
code. Host I/O errors, cancellation and deadline failures are `tool-error` and
cannot be declared an expected passing outcome.

## Bounds

| Limit | Default | Maximum |
| --- | ---: | ---: |
| `maxSteps` | 1,000 | 100,000 |
| `timeoutMs` | 2,000 | 5,000 |
| `maxOutputBytes` | 65,536 | 65,536 |
| `maxObjectAllocations` | 10,000 | 10,000 |

Zero selects the default; negative and excessive limits are invalid. The VM
retains at most 128 trace entries / 1 MiB and uses the remaining public default
policies. Per-case deadlines cover source loading, compilation and execution;
all cases share a 30-second context. Cancellation is cooperative through the
public facade and cannot interrupt a blocked OS file read or an individual
compiler phase immediately. This is local testing, not an OS security sandbox.

The UTF-8 manifest must be a regular file of at most 1 MiB with 1–100 uniquely
named cases. Unknown fields, unsupported schema versions and trailing JSON are
rejected. Cases require 1–32 ordered `.cl` files totaling at most 2 MiB, with
forward-slash relative paths. Sources must be regular files whose resolved
paths stay inside the manifest directory, including through symlinks. Input
and expected output are each limited to 64 KiB. Duplicate normalized source
paths are rejected. These checks assume files are not concurrently replaced.

## Results and exit status

Text mode prints each PASS/FAIL and a final count. `--json` writes a separate
version-1.0.0 test-result object containing compiler version, counts and ordered
cases with actual/expected outcome, output, diagnostic pairs, executed steps
and failure reasons. It is not a complete compilation report. Redirect stdout
to save it; usage and manifest errors go to stderr.

- `0`: every case passed, including expected failures.
- `1`: one or more expected/actual mismatches.
- `2`: command usage or invalid manifest.
- `3`: file I/O, output writing, deadline/cancellation or internal tool failure.

After a tool failure, later cases still receive result entries; an expired
shared context makes those entries fail immediately. A tool failure takes exit
status precedence over ordinary mismatches.
