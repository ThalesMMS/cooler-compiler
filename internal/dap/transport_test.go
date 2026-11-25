// SPDX-License-Identifier: AGPL-3.0-or-later
package dap

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestFraming(t *testing.T) {
	for _, input := range []string{"Content-Length: 9999999\r\n\r\n", "Content-Length: 2\r\nContent-Length: 2\r\n\r\n{}", "Bad\r\n\r\n", "Content-Length: 2\r\n\r\n{"} {
		if _, err := readMessage(bufio.NewReader(bytes.NewBufferString(input))); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
	got, err := readMessage(bufio.NewReader(bytes.NewBufferString("Content-Length: 2\r\n\r\n{}")))
	if err != nil || string(got) != "{}" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := readMessage(bufio.NewReader(bytes.NewReader(nil))); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}
