# Supported COOL language and compatibility

This document is the language-level contract for Cooler v0.1. The constructs below are implemented across the lexer, parser, semantic analyzer, stack IR, pseudo-assembly formatter, and Go VM. The implementation is complete within this declared educational scope; it is not a promise that Cooler can replace every artifact or invocation of the reference COOL compiler and runtime.

For the tooling feature matrix and concise release boundary, see [Scope and compatibility in the README](../README.md#scope-and-compatibility).

## Program structure

```ebnf
program     = { class ";" } ;
class       = "class" TYPE [ "inherits" TYPE ] "{" { feature ";" } "}" ;
feature     = OBJECT "(" [ formal { "," formal } ] ")" ":" TYPE "{" expr "}"
            | OBJECT ":" TYPE [ "<-" expr ] ;
formal      = OBJECT ":" TYPE ;
```

Classes inherit from `Object` when no parent is written. Inheritance from `Int`, `String`, `Bool`, or `SELF_TYPE` is rejected. Attributes cannot be redefined by descendants. Method overrides must preserve the complete formal-type list and return type.

A runnable program must contain class `Main` and a parameterless `main` method.

The `coolerc` CLI may supply several `.cl` files; their class definitions are concatenated, in argument order, into one program. Cool has no separate modules.

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

`IO.in_string` and `IO.in_int` consume program-input lines when they are provided. The GUI has a Program input box in the console panel; only redirected or piped stdin supplies program-input lines after file arguments (`coolerc program.cl < input.txt`, or several files such as `coolerc stack.cl atoi.cl < stack.test`; interactive TTY is empty). With no input they return `""` and `0`, which keeps debugging deterministic.

## Lexical details

- type identifiers begin with an uppercase letter;
- object identifiers begin with a lowercase letter;
- keywords are case-insensitive;
- `true` and `false` require a lowercase initial letter;
- `--` begins a line comment;
- `(* ... *)` block comments may be nested;
- strings support escaped newline, tab, backspace, form feed, quote, and backslash;
- source locations preserve line, rune-column, and UTF-16 offsets.

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

- Cooler lowers supported programs to source-linked stack bytecode, control-flow metadata, and educational pseudo-assembly. It does not emit MIPS, `.s` files for SPIM, native object code, or linker inputs.
- Execution uses Cooler's own Go VM. The reference COOL runtime ABI, object layout, command-line flags, diagnostic wording, and debugger protocols are not compatibility targets.
- Non-negative integer literals must fit the signed 32-bit literal range (`0` through `2147483647`; negative values use `~`). Runtime arithmetic uses the host Go `int`, so overflow does not reproduce strict 32-bit wrapping behavior.
- VM objects and strings are owned by Go's memory manager. Cooler does not implement or expose the reference runtime's collector selection, heap layout, or GC stress options.
- Program input is a pre-supplied sequence of lines. `IO.in_string` returns the next line and `IO.in_int` trims and parses the next line. Exhaustion returns `""`/`0`, and an invalid integer line returns `0`.
- In the GUI, the Program input box supplies those lines. In the CLI, redirected or piped stdin supplies program input only when one or more source-file arguments are present. With no file arguments, stdin is the COOL source; an interactive TTY is deliberately not consumed as program input.
- Multifile compilation concatenates complete class declarations in argument/tab order and maps diagnostics back to their original file. COOL modules, imports, separate compilation, object files, and linking are non-goals.
- The VM deliberately caps execution at 100,000 instructions, 4,096 operand-stack values, 256 call frames, 10,000 allocated objects, and 5,000 retained trace entries. Reaching a limit produces a Cooler runtime diagnostic.
