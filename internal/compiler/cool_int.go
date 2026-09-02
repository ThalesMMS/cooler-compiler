// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"strconv"
	"strings"
)

// coolInt is the architecture-independent representation of a COOL Int.
// Arithmetic is explicitly modulo 2^32 and values are displayed as signed
// two's-complement decimals.
type coolInt int32

const (
	coolIntMin            coolInt = -1 << 31
	coolIntMax            coolInt = 1<<31 - 1
	maxCoolIntegerLiteral         = "2147483647"
)

func validIntegerLiteral(literal string) bool {
	normalized := strings.TrimLeft(literal, "0")
	if normalized == "" || len(normalized) < len(maxCoolIntegerLiteral) {
		return true
	}
	return len(normalized) == len(maxCoolIntegerLiteral) && normalized <= maxCoolIntegerLiteral
}

func parseCoolIntLiteral(literal string) (coolInt, bool) {
	if !validIntegerLiteral(literal) {
		return 0, false
	}
	value, err := strconv.ParseUint(literal, 10, 31)
	if err != nil {
		return 0, false
	}
	return coolInt(value), true
}

func parseCoolIntInput(input string) (coolInt, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(input), 10, 32)
	if err != nil {
		return 0, false
	}
	return coolInt(value), true
}

func coolIntFromBits(bits uint32) coolInt { return coolInt(int32(bits)) }

func coolIntFromLength(length int) coolInt {
	return coolIntFromBits(uint32(uint64(length)))
}

func (value coolInt) add(other coolInt) coolInt {
	return coolIntFromBits(uint32(value) + uint32(other))
}

func (value coolInt) subtract(other coolInt) coolInt {
	return coolIntFromBits(uint32(value) - uint32(other))
}

func (value coolInt) multiply(other coolInt) coolInt {
	return coolIntFromBits(uint32(value) * uint32(other))
}

func (value coolInt) divide(other coolInt) (coolInt, bool) {
	if other == 0 {
		return 0, false
	}
	if value == coolIntMin && other == -1 {
		return coolIntMin, true
	}
	return coolInt(int32(value) / int32(other)), true
}

func (value coolInt) complement() coolInt {
	return coolIntFromBits(0 - uint32(value))
}

func (value coolInt) less(other coolInt) bool      { return value < other }
func (value coolInt) lessEqual(other coolInt) bool { return value <= other }
func (value coolInt) equal(other coolInt) bool     { return value == other }
func (value coolInt) String() string               { return strconv.FormatInt(int64(value), 10) }
