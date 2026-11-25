// SPDX-License-Identifier: AGPL-3.0-or-later
package dap

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const maxMessage = 1048576

func readMessage(reader *bufio.Reader) ([]byte, error) {
	length := -1
	total := 0
	for {
		raw, err := reader.ReadSlice('\n')
		total += len(raw)
		line := string(raw)
		if err != nil {
			return nil, err
		}
		if total > 8192 {
			return nil, fmt.Errorf("DAP headers exceed 8 KiB")
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("invalid DAP header")
		}
		if strings.EqualFold(key, "Content-Length") {
			if length >= 0 {
				return nil, fmt.Errorf("duplicate Content-Length")
			}
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil || length < 1 || length > maxMessage {
				return nil, fmt.Errorf("invalid DAP message length")
			}
		}
	}
	if length < 1 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	body := make([]byte, length)
	_, err := io.ReadFull(reader, body)
	return body, err
}
