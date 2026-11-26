# COOL formatting

`coolerc fmt file.cl` prints canonical source. With no file, it reads stdin.
`coolerc fmt -check file.cl other.cl` returns 0 when all files are formatted,
1 for differences or invalid source, 2 for invalid usage and 3 for tool I/O errors.
`coolerc fmt -w file.cl` explicitly replaces one regular file using a synced
temporary file in the same directory and rename. It preserves permission bits
and refuses symlinks. Parse/format failures leave the original intact. Batch
writes are refused; use multi-file checking and format files individually so
there is no misleading promise of a cross-file filesystem transaction.

The style uses two spaces per brace level, newlines after opening braces and
semicolons and before closing braces, one separating space and LF outside
preserved literals/comments. Parentheses and expression token order remain
unchanged, preserving precedence. Let/case expressions retain their tokens;
there is no AST reconstruction or partial formatting. Strings, escapes, Unicode,
line comments and nested block comments are copied verbatim and in order.
Comments finish a line; multi-line comment interiors retain original whitespace.
CRLF outside preserved regions becomes LF. A final token comparison prevents
accidental token fusion. Input and incremental output are bounded to 1 MiB.

LSP `textDocument/formatting` returns a whole-document UTF-16 edit, or no edits
when already formatted. Invalid syntax returns an error. Generation and
cancellation checks discard stale results. The canonical style is fixed rather
than dependent on editor tab settings. The GUI Format button applies only to the
active unchanged document, retains selection around token boundaries (including
spaces inside literals), marks the source dirty, saves its recovery copy and
invalidates the prior compiler/debugger session. Disk saving remains explicit.

