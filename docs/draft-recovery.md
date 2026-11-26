# GUI draft recovery

The GUI saves recovery copies of tab names, ordered source contents and the active
tab after a 300 ms debounce. On startup, **Restore drafts** reopens the saved tabs;
**Discard drafts** removes the offered recovery copy. **Clear recovery data** is
always available and leaves the current editor unchanged. Only subsequent tab or
content changes recreate the copy; compiling or repainting alone does not.

Recovered tabs show `recovered` and are dirty even if their content resembles a
saved file. Recovery does not overwrite source files or recover browser file
handles. Use Save/Save As to write each file. The recovery payload excludes HTTP
session IDs, capabilities, input, executable VM state and debugger objects.

## Location and durability

Both browser and desktop modes use the local GUI service's protected
`POST /api/drafts` endpoint, not origin-scoped localStorage. The default file is
`cooler-compiler/drafts.json` below Go's `os.UserConfigDir`: normally
`%AppData%` on Windows, `~/Library/Application Support` on macOS, and
`$XDG_CONFIG_HOME` or `~/.config` on Linux. `coolergui --draft-dir=/path` selects
an isolated directory. Port/origin changes do not change that directory.

The service writes a temporary file in the same directory, syncs and closes it,
then renames it over the destination. Unix directories/files are created with
0700/0600 permissions; Windows inherits the user's directory ACL. An OS-held lock
(`LockFileEx` on Windows, flock on Unix) spans read/revision-check/write, including
across processes. The OS releases it on handle close or process termination.
Other platforms report unsupported storage instead of pretending to persist.

The file contains a revision and an optional version-1 workspace. Saves/clears
must match the current revision; stale writers receive 409 and stop autosaving.
Lock contention or unavailable storage is reported and pauses recovery saving.
Reload to re-read the current copy before continuing. Clearing writes a tombstone
with an incremented revision so another stale window cannot recreate deleted data.
The empty lock file is coordination metadata, not recovery content.
Explicit clearing also removes orphaned `.draft-*` temporary files left by an
interrupted write while holding the storage lock.

The status **Recovery copy saved** means the latest queued snapshot was
acknowledged, not that a source file was saved. New changes during a write are
serialized immediately after it; the UI remains in “saving” until the queue is
empty. Page hiding attempts a flush; requests below 60 KiB use fetch keepalive.
The last acknowledged snapshot survives process interruption. Changes within the
debounce or an unfinished request can still be lost on abrupt shutdown, especially
large payloads or a close during another write. This is bounded autosave, not a
transactional keystroke journal or a guarantee against sudden power/storage loss.

## Bounds and failure behavior

At most 16 tabs, 128 UTF-8 bytes per name, 128 KiB per tab, 256 KiB source content
in total, and 2 MiB encoded JSON are accepted. Active index and format version are
validated. Unknown fields, trailing data, invalid UTF-8, malformed/corrupt JSON
and unsupported versions are rejected. Errors do not replace editor content or
overwrite the last good copy. Corrupt recovery can be explicitly cleared from
the UI. No paths from draft names are opened by the service.

API actions are `read`, `save` and `clear`; all require the current GUI capability
and normal host/origin checks. Save adds `{revision, workspace}`; clear adds
`revision`; read returns `{revision, workspace}`. Storage errors use 503 and invalid
requests use 400. The capability is transported only in the request header.
