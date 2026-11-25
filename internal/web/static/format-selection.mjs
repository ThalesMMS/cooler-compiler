// SPDX-License-Identifier: AGPL-3.0-or-later
// Formatting changes only whitespace. Keep each endpoint at the same non-space
// character boundary, using JavaScript UTF-16 offsets as the textarea does.
export function mapFormatSelection(before, after, position, affinity = 'right') {
  let count = 0;
  let spaces = 0;
  for (let i = 0; i < position; i++) {
    if (!/\s/u.test(before[i])) { count++; spaces = 0; } else spaces++;
  }
  let offset = 0;
  while (offset < after.length && count > 0) {
    if (!/\s/u.test(after[offset])) count--;
    offset++;
  }
  while (spaces > 0 && offset < after.length && /\s/u.test(after[offset])) { offset++; spaces--; }
  if (affinity === 'right' && position < before.length && !/\s/u.test(before[position])) {
    while (offset < after.length && /\s/u.test(after[offset])) offset++;
  }
  return offset;
}
