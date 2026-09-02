# Supported COOL language and compatibility

This document is the language-level contract for Cooler v1.0.0. The constructs below are implemented across the lexer, parser, semantic analyzer, stack IR, pseudo-assembly formatter, and Go VM. The implementation is complete within this declared educational scope; it is not a promise that Cooler can replace every artifact or invocation of the reference COOL compiler and runtime.

For the tooling feature matrix and concise release boundary, see [Scope and compatibility in the README](../README.md#scope-and-compatibility).

## Program structure

```ebnf
program     = { class ";" } ;
class       = "class" TYPE [ "inherits" TYPE ] "{" { feature ";" } "}" ;
feature     = OBJECT "(" [ formal { "," formal } ] ")" ":" TYPE "{" expr "}"
            | OBJECT ":" TYPE [ "<-" expr ] ;
formal      = OBJECT ":" TYPE ;
```

Classes inherit from `Object` when no parent is written. Basic classes cannot be
redefined, `SELF_TYPE` cannot name a class, and inheritance from `Int`, `String`,
`Bool`, or `SELF_TYPE` is rejected. Undefined parents and direct or indirect
inheritance cycles are errors. Cycle members recover to `Object` for subsequent
queries, so independent errors can still be reported without recursion loops.

Attributes cannot be named `self`, repeated in one class, or redefined by a
descendant. Their declared types must exist and explicit initializers must
conform. Methods cannot repeat within a class. Formal names are unique and
cannot be `self`; formal types must be defined concrete classes rather than
`SELF_TYPE`. Return types may be `SELF_TYPE`. Overrides preserve the complete
formal-type list and return type exactly.

A runnable program must contain class `Main` and that class must declare, rather
than merely inherit, a parameterless `main` method. Its return type must be
defined and its body must conform to that declaration.

The `coolerc` CLI may supply several ordered `.cl` source units. Each is lexed and parsed with file-local coordinates before complete class declarations are combined into one program. Cool has no separate modules.

### Class-table order and recovery

Semantic analysis uses six deterministic passes: collect classes in composed
source order; validate parents and recover cycles; collect features in source
order; validate inherited features; validate `Main.main`; then type-check
attribute initializers and method bodies. The class table exposes `Order`,
`AttributeOrder`, and `MethodOrder`; maps are used only for lookup and never
decide exported artifact or diagnostic order. Duplicate diagnostics relate back
to the first declaration, and invalid overrides relate to the inherited feature.

All ancestry operations are cycle guarded. Cycle discovery is iterative, so a
deep hierarchy does not consume one Go call frame per class. A semantic error is
a hard backend barrier: the analyzed AST and symbols remain inspectable, but IR,
assembly, and VM preparation do not run.

Allocation installs the COOL defaults `0`, `""`, `false`, or void for every
inherited and local attribute. Explicit initializers then execute ancestors
before descendants and use source declaration order within each class.

### Parser recovery and partial AST

The parser synchronizes at class, feature, and case-branch boundaries. A missing
class close does not consume the next `class`, and every recovery loop either
advances or returns at a boundary. Parsing is capped at 512 nested expression
levels and 100 diagnostics (`COL2110` and `COL2111`) so adversarial input remains
bounded.

The GUI may receive a partial AST after parser errors. `NoExpr` with text
`<no expression>` is the only synthetic expression node and explicitly marks a
required expression that could not be recovered. Ordinary class, feature, and
expression nodes are retained only when their semantic components (names,
types, operands, and bodies) came from source; missing punctuation or closing
keywords may still leave such a structurally valid recovered node. Their spans
cover the observed construct and remain valid UTF-16 source ranges.

Any parser error is a hard frontend barrier: the partial AST is inspectable, but
semantic analysis, IR, assembly, and VM preparation do not run. Diagnostics stay
in deterministic encounter order, and parser recovery never mutates or reorders
earlier lexer diagnostics.

## Expressions

The compiler supports:

```cool
identifier <- expression
receiver.method(arguments)
receiver@Ancestor.method(arguments)
method(arguments)                  -- implicit self
if condition then a else b fi
while condition loop body pool
{ expression; expression; }
let x : T <- value, y : U in body
case value of x : T => body; esac
new Type
new SELF_TYPE
isvoid expression
~integer
not boolean
+ - * / < <= =
```

Expression static types are attached to AST nodes. `if` and `case` use the least upper bound of branch types. `while` has static type `Object`. A `let` binding without an initializer receives `0`, `""`, `false`, or void according to its declared type.

### Static expression semantics

Names resolve from the innermost `let`/`case` scope through formals, then
attributes; `self` always denotes the current receiver and has static type
`SELF_TYPE`. Assignment may target a local, formal, or attribute, but never
`self`. A successful assignment has the right-hand expression's type.

Dispatch lookup uses the receiver's static type. Implicit dispatch starts at
the current class; dynamic dispatch preserves the receiver type when the method
returns `SELF_TYPE`; static dispatch through `expr@T` substitutes `T` for that
return. The receiver must conform to `T`, and `T` must be a defined concrete
class. Method availability, arity, and argument conformance are checked before
backend generation.

At runtime, dispatch arguments are evaluated from left to right before the
receiver expression, matching the manual's operational rule. The resulting
receiver is then checked for void and used for dynamic or explicit static
method selection. This ordering applies even when argument and receiver
expressions both have observable side effects.

Multi-binding `let` is sequential: each initializer is analyzed before its own
name enters scope, then later bindings may see and shadow it. Repeating a name
therefore behaves like nested single-binding lets. A `case` branch variable is
visible only in its branch. Branch types are concrete, defined, and unique; the
static result is the least upper bound of every body and does not depend on
branch source order.

`SELF_TYPE_C` conforms to `C` and its ancestors, while a concrete `C` does not
conform back to `SELF_TYPE_C`. The LUB of two `SELF_TYPE_C` values remains
`SELF_TYPE_C`; mixing it with another type first resolves it to `C`. Attributes,
method returns, `new`, and assignment may use `SELF_TYPE` as documented;
classes, parents, formals, case branches, and static-dispatch type names may not.

If an expression is already invalid, its AST type is `<error_type>`. This type
is absorbing for LUB and accepted only as an internal conformance-recovery
sentinel: parents continue visiting independent siblings but do not emit a
second diagnostic caused solely by the invalid child. Parser-only `NoExpr`
instead carries `<no_type>` and cannot reach semantic analysis because of the
frontend barrier.

## Built-in classes

```text
Object
  abort() : Object
  type_name() : String
  copy() : SELF_TYPE

IO inherits Object
  out_string(x : String) : SELF_TYPE
  out_int(x : Int) : SELF_TYPE
  in_string() : String
  in_int() : Int

String inherits Object
  length() : Int
  concat(s : String) : String
  substr(i : Int, l : Int) : String

Int inherits Object
Bool inherits Object
```

`IO.in_string` and `IO.in_int` consume program-input lines when they are provided. The transport removes the terminating CRLF or LF delimiter and `in_string` preserves the remaining content, including whitespace and an embedded NUL. The GUI has a pre-supplied Program input box and, when execution reaches an exhausted read, a separate live-input form. Redirected or piped stdin supplies pre-supplied lines after file arguments (`coolerc program.cl < input.txt`, or several files such as `coolerc stack.cl atoi.cl < stack.test`); `coolerc run --interactive program.cl` instead reads each requested line live after displaying preceding output. With no input or after interactive EOF they return `""` and `0`, which keeps non-interactive execution deterministic. `in_int` accepts a trimmed signed decimal in `-2147483648..2147483647`; an invalid or out-of-range line returns `0`. `out_int` emits the canonical signed decimal form.

## Lexical details

Cooler follows sections 7.1 and 10 of the canonical manual with the explicit
source-format and recovery decisions below:

- identifiers use ASCII letters, digits, and underscore. Type identifiers begin
  with `A`-`Z`, object identifiers begin with `a`-`z`, and underscore is allowed
  only after that first letter. `self` remains an object identifier and
  `SELF_TYPE` remains a type identifier rather than becoming a keyword;
- keywords are case-insensitive. `true` and `false` require a lowercase first
  letter, while their remaining letters are case-insensitive. A keyword embedded
  in a longer identifier is an identifier;
- integer literals are non-empty decimal digit sequences. Leading zeroes are
  accepted. The lexer accepts values through `2147483647`, reports `COL1007`
  above that boundary without numeric conversion overflow, and supplies a zero
  recovery value so the parser does not repeat the error;
- `--` begins a line comment and `(* ... *)` block comments may be nested.
  Delimiters inside strings or the other comment form have no special meaning;
- strings decode `\b`, `\t`, `\n`, and `\f`; every other `\c` decodes to `c`.
  A backslash followed by LF, CRLF, or CR decodes to one newline. Unescaped line
  breaks, EOF, raw NUL, and escaped NUL are rejected. The decoded value is at
  most 1024 Unicode characters, and scanning continues to the closing quote
  without retaining an unbounded recovery buffer;
- grammar and whitespace outside strings are ASCII. The whitespace characters
  are space, LF, form feed, CR, tab, and vertical tab. Unicode is allowed in
  string values; other non-ASCII grammar runes are rejected. Source bytes must
  be UTF-8, and each malformed byte produces `COL1008` and consumes one recovery
  position;
- LF, CRLF, and bare CR each advance one logical source line. Lines and columns
  are 1-based, end positions are exclusive, and columns count decoded Unicode
  runes. Offsets are 0-based UTF-16 code units for JavaScript selection: a BMP
  rune counts as one and a supplementary rune such as an emoji counts as two.
  Byte positions are not serialized; they are obtained by UTF-8-encoding the
  source prefix, so they can differ from both rune columns and UTF-16 offsets.

Lexer diagnostics are emitted in source encounter order. A malformed string
emits at most one fatal string diagnostic, then consumes through its recovery
boundary. Recoverable invalid characters, identifiers, and integers preserve
later tokens deterministically.

## Precedence, tightest to loosest

```text
.                     dynamic dispatch
@                     static dispatch
~
isvoid
* /
+ -
< <= =                 non-associative
not
<-                    right-associative
```

## Compatibility boundaries and non-goals

- Cooler lowers supported programs to source-linked stack bytecode, control-flow metadata, and educational pseudo-assembly, then verifies backend invariants before VM construction. It does not emit MIPS, `.s` files for SPIM, native object code, or linker inputs.
- Execution uses Cooler's own Go VM. The reference COOL runtime ABI, object layout, command-line flags, diagnostic wording, and debugger protocols are not compatibility targets.
- Non-negative integer literals must fit `0..2147483647`; this is a source rule, and negative values use `~`. Runtime `Int` values are always signed 32-bit two's-complement values. Addition, subtraction, multiplication, and `~` wrap modulo 2^32. Division truncates toward zero, division by zero is `COL6015`, and the otherwise overflowing `-2147483648 / -1` deterministically returns `-2147483648`. Ordering and equality compare the wrapped signed values. The manual does not define overflow behavior, so modular arithmetic is an explicit architecture-independent Cooler policy informed by the classic 32-bit runtime representation, not a claim that every reference tool reports overflow identically.
- VM objects and strings are owned by Go's memory manager. Cooler does not implement or expose the reference runtime's collector selection, heap layout, or GC stress options.
- The classic manual defines String operations in characters without specifying
  Unicode encoding. Cooler accepts Unicode string values as an extension and
  defines `length`/`substr` indices in decoded code points, not bytes or UTF-16.
- Program input is a pre-supplied sequence of lines. `IO.in_string` returns the next line and `IO.in_int` trims and parses the next line. Exhaustion returns `""`/`0`, and an invalid or out-of-range signed 32-bit integer line returns `0`.
- In the GUI, the Program input box supplies those lines. In the CLI, redirected or piped stdin supplies program input only when one or more source-file arguments are present. With no file arguments, stdin is the COOL source; an interactive TTY is deliberately not consumed as program input.
- Multifile compilation preserves ordered native source units and stable file-local spans throughout compilation and debugging. COOL modules, imports, separate compilation, object files, and linking are non-goals.
- The VM deliberately caps execution at 100,000 instructions, 4,096 operand-stack values, 256 call frames, 10,000 allocated objects, and 5,000 retained trace entries. Reaching a limit produces a Cooler runtime diagnostic.
