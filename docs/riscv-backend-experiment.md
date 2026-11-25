# Educational RISC-V backend experiment (#87)

Decision: retain a test-only RV32IM emitter and the native VM as the production
runtime. This experiment emits real RARS assembly from verified Stack IR.
The existing pseudo-assembly display is unchanged and is not executable RISC-V.

## Local environment and reproduction

The executable target is [RARS 1.6](https://github.com/TheThirdOne/rars/releases/tag/v1.6)
in its default RV32 mode, with a local Java runtime (Java 8 or later). Download
the release's `rars1_6.jar` yourself; the project neither bundles nor downloads it
during tests. The tested JAR SHA-256 is
`780f730eb457b1ba609e968accc2c8b77d8f92c3d9dbf30cc7fdb3cfb14e8c24`.

```sh
COOLER_RARS_JAR=/absolute/path/rars1_6.jar \
  go test ./internal/compiler -run '^TestRISCVBackendPrototype' -v -count=1
```

The tests generate temporary `.s` files, then actually assemble and execute them:

```sh
java -Djava.awt.headless=true -jar /absolute/path/rars1_6.jar \
  nc me ae2 se3 10000 program.s
```

`nc` suppresses the banner, `me` separates simulator messages from program output,
and assembly/simulation failures use distinct nonzero statuses. Execution has a
10,000-machine-instruction cap and each child process a 20-second outer deadline.
See [RARS command-line options](https://github.com/TheThirdOne/rars/blob/v1.6/src/help/Command.html).
Unset COOLER_RARS_JAR explicitly skips simulator execution. Emitter refusal tests
still run, and setting the variable without Java fails rather than silently skips.

Validated locally on Windows through WSL Debian, OpenJDK 21.0.12.1, RARS 1.6,
with the Go 1.26.4 Linux test executable cross-built on Windows. Eleven execution
cases finished in 2.79 seconds. Native Windows Java and other simulators were not
available/validated. No MIPS/SPIM, Linux syscall ABI, hardware or psABI compatibility
is claimed. RARS is [MIT licensed](https://github.com/TheThirdOne/rars/blob/master/License.txt);
OpenJDK uses GPLv2 with the Classpath Exception. Neither is distributed here.

## ISA and observable contracts

RV32IM was chosen because COOL Int has 32-bit wraparound and this educational
slice benefits from direct `mul` and `div`. Those instructions belong to
[extension M](https://docs.riscv.org/reference/isa/unpriv/m-st-ext.html), not RV32I.
The RISC-V division-overflow result matches COOL's INT_MIN / -1 result. Hardware
division by zero does not implement COOL's error, so a branch precedes every div.
The error path exits with status 15, representing COL6015. It preserves output
already produced. Success exits 0; simulator failures use statuses 2 and 3.

The emitter attaches a comment with original IR index, opcode, source ID and
line/column range to every instruction expansion. Source IDs are quoted/escaped
so embedded newlines cannot become assembly instructions (regression tested).
A division error has a stable
status but does not yet return a machine-readable source span or debugger frame.
The native VM retains the richer diagnostic; no parity is claimed for messages,
frame counts, retained steps or resource accounting.

The comparison corpus executes multiplication, multiplication/addition overflow,
negative division, division overflow, zero division after prior output, ordered
side effects and nested/shadowed Int locals. Exact stdout and success/error
status are checked against the VM. Examples: 7*6 → `42`; INT_MAX*2 → `-2`;
ordered effects → `136`; output before zero division → `12`, exit 15.
Three additional Boolean cases observe the otherwise discarded main result
through a test-only PrintInt insertion, comparing NOT, ordering and identity to
the VM. Generated assembly sizes for the eight output cases were 909–1,749 bytes including comments.
These are text sizes, not machine-code or executable sizes. No speed comparison
or performance advantage is asserted.

## Executable subset and private convention

Only the user class Main, without attributes or helper methods, is accepted.
Its main body is straight-line and at most 256 IR instructions. Int/Bool locals,
scalar operations, comparisons, lexical scopes and IO.out_int on direct self
are supported. Everything else fails before assembly is returned: allocation,
strings, loops, conditionals, case, arbitrary calls, Object-typed locals and
assignment. Explicit IO static dispatch is resolved through the class table.

The singleton receiver has identity 1, and Int/Bool values are unboxed words.
This is a private educational representation, not the future general object ABI.
At entry, sp is aligned to 16 bytes and reserves 1,024 bytes for local words;
s0 points to that fixed area. Each operand uses a 16-byte stack slot below it.
The 256-instruction acceptance bound limits both local count and operand depth;
there are no loops, allocation or recursive calls in this slice. t0/t1 are scratch,
a0 carries output or exit status and a7 selects the RARS environment call.

IO.out_int pops receiver then argument, invokes PrintInt (a7=1), and restores the
receiver as its result. RARS preserves registers other than a call's specified
output registers; this assumption is part of the
[environment contract](https://github.com/TheThirdOne/rars/wiki/Environment-Calls).
Exit2 uses a7=93. No input, filesystem, network or allocation calls are emitted.
The COOL main result is discarded, like a process entry result, while explicit
out_int calls provide program stdout. The assembly is for standalone simulation,
not linking with C or an external runtime.

## Expansion design and adoption gate

General methods need an explicit ABI: a0 for receiver/result, a1–a7 for the first
arguments, overflow arguments in aligned caller stack storage, and callee frames
saving ra/s0 and any used saved registers. Keep expression operands separate from
argument staging so argument evaluation remains in COOL's order. Add depth checks
before each call and prove nested calls preserve live values.

General objects need a header with class tag, object size and dispatch table
pointer, followed by inherited attributes in class-table order. Void is zero.
Box Int/Bool on flows to Object and distinguish primitive equality from identity.
Dynamic dispatch loads a stable method slot; static dispatch calls the declared
ancestor implementation directly. SELF_TYPE allocation selects the receiver's
dynamic class. Run inherited initializers in the native VM order.

Strings need immutable length plus UTF-8 bytes and bounded allocation; port the
existing runtime's Unicode behavior and error cases before accepting them.
Implement IO through a narrow runtime shim with explicit input completion and
output quotas. Begin allocation with a fixed checked arena and a documented
no-reclamation policy; evaluate a collector only after measuring live objects.
RARS sbrk alone would not enforce the VM's object/byte limits.

General CFG can lower verified blocks to labels and branches, with consistent
operand stack state at every join. Case needs class-tag ancestry tests and a
most-specific match. Each failure should carry its diagnostic code and source
span through a structured host protocol. Instruction budgets and debugger
history require additional runtime state; simulator machine-instruction limits
are containment, not VM policy equivalence.

Before any production adoption: differential conformance for all accepted
features, object/dispatch/Unicode/error-order fixtures, ABI stress tests, bounded
memory and input, machine-code inspection and independent simulator validation.
This prototype answers feasibility for a documented slice; it is not full COOL.
