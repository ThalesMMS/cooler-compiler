// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	jsonRPCParseError      = -32700
	jsonRPCInvalidRequest  = -32600
	jsonRPCMethodNotFound  = -32601
	jsonRPCInvalidParams   = -32602
	jsonRPCInternalError   = -32603
	jsonRPCServerNotInit   = -32002
	jsonRPCRequestCancel   = -32800
	jsonRPCContentModified = -32801
	jsonRPCServerBusy      = -32000
	maxLSPHeaderBytes      = 16 << 10
	maxRequestIDBytes      = 256
)

var (
	errHeadersTooLarge = errors.New("LSP headers exceed configured limit")
	errMessageTooLarge = errors.New("LSP message exceeds configured limit")
)

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

func readFrame(reader *bufio.Reader, maximum int) ([]byte, error) {
	contentLength := -1
	headerBytes := 0
	for {
		var line bytes.Buffer
		for {
			fragment, err := reader.ReadSlice('\n')
			headerBytes += len(fragment)
			if headerBytes > maxLSPHeaderBytes {
				return nil, fmt.Errorf("%w: %d bytes", errHeadersTooLarge, headerBytes)
			}
			_, _ = line.Write(fragment)
			if err == nil {
				break
			}
			if !errors.Is(err, bufio.ErrBufferFull) {
				return nil, err
			}
		}
		trimmed := strings.TrimRight(line.String(), "\r\n")
		if trimmed == "" {
			break
		}
		name, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return nil, fmt.Errorf("malformed LSP header")
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			if contentLength >= 0 {
				return nil, fmt.Errorf("duplicate Content-Length")
			}
			parsed, parseErr := strconv.Atoi(strings.TrimSpace(value))
			if parseErr != nil || parsed < 0 {
				return nil, fmt.Errorf("invalid Content-Length")
			}
			contentLength = parsed
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	if contentLength > maximum {
		return nil, fmt.Errorf("%w: %d bytes", errMessageTooLarge, contentLength)
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeFrame(writer io.Writer, value any) error {
	return writeFrameMeasured(writer, value, nil)
}

func writeFrameMeasured(writer io.Writer, value any, measurement *measurementProbe) error {
	serializationDone := beginMeasurement(measurement, "serialization")
	payload, err := json.Marshal(value)
	serializationDone()
	if err != nil {
		return err
	}
	header := []byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload)))
	writeDone := beginMeasurement(measurement, "publication-write")
	_, err = io.Copy(writer, io.MultiReader(bytes.NewReader(header), bytes.NewReader(payload)))
	writeDone()
	return err
}

func decodeParams(data json.RawMessage, destination any) error {
	if len(data) == 0 || string(data) == "null" {
		data = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func requestKey(id json.RawMessage) string { return string(id) }

func hasRequestID(id json.RawMessage) bool {
	return len(id) > 0
}

func validRequestID(id json.RawMessage) bool {
	if !hasRequestID(id) || len(id) > maxRequestIDBytes {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(id))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return false
	}
	switch value := value.(type) {
	case string:
		return true
	case json.Number:
		_, err := value.Int64()
		return err == nil
	default:
		return false
	}
}
