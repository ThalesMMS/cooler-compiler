// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"sort"
	"strings"
)

const (
	maxDebugExpressionBytes = 1_024
	maxDebugExpressionNodes = 256
)

type parsedDebugExpression struct {
	expr         Expr
	dependencies []string
}

func parseDebugExpression(source string) (*parsedDebugExpression, error) {
	if strings.TrimSpace(source) == "" {
		return nil, fmt.Errorf("expression is empty")
	}
	if len(source) > maxDebugExpressionBytes {
		return nil, fmt.Errorf("expression exceeds %d bytes", maxDebugExpressionBytes)
	}
	tokens, diagnostics := LexSource("debugger", source)
	if len(diagnostics) > 0 {
		return nil, fmt.Errorf("%s", diagnostics[0].Message)
	}
	p := &parser{tokens: tokens}
	expr := p.parseExpression()
	if len(p.diagnostics) > 0 {
		return nil, fmt.Errorf("%s", p.diagnostics[0].Message)
	}
	if !p.atEnd() {
		return nil, fmt.Errorf("unexpected %s after expression", tokenDisplayName(p.peek().Type))
	}
	dependencies := map[string]bool{}
	nodes := 0
	if err := validateDebugExpression(expr, dependencies, &nodes); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(dependencies))
	for name := range dependencies {
		names = append(names, name)
	}
	sort.Strings(names)
	return &parsedDebugExpression{expr: expr, dependencies: names}, nil
}

func validateDebugExpression(expr Expr, dependencies map[string]bool, nodes *int) error {
	*nodes++
	if *nodes > maxDebugExpressionNodes {
		return fmt.Errorf("expression exceeds the %d-node evaluation limit", maxDebugExpressionNodes)
	}
	switch value := expr.(type) {
	case *IdentifierExpr:
		if value.Name != "self" && value.Name != "void" {
			dependencies[value.Name] = true
		}
		return nil
	case *IntExpr, *StringExpr, *BoolExpr:
		return nil
	case *IsVoidExpr:
		return validateDebugExpression(value.Value, dependencies, nodes)
	case *UnaryExpr:
		if value.Operator != TokenNot && value.Operator != TokenTilde {
			return fmt.Errorf("operator %s is not allowed in debugger expressions", value.Operator)
		}
		return validateDebugExpression(value.Value, dependencies, nodes)
	case *BinaryExpr:
		switch value.Operator {
		case TokenPlus, TokenMinus, TokenStar, TokenSlash, TokenLess, TokenLessEqual, TokenEqual:
		default:
			return fmt.Errorf("operator %s is not allowed in debugger expressions", value.Operator)
		}
		if err := validateDebugExpression(value.Left, dependencies, nodes); err != nil {
			return err
		}
		return validateDebugExpression(value.Right, dependencies, nodes)
	case *AssignExpr:
		return fmt.Errorf("assignment is not allowed in debugger expressions")
	case *DispatchExpr:
		return fmt.Errorf("method dispatch is not allowed in debugger expressions")
	case *NewExpr:
		return fmt.Errorf("object allocation is not allowed in debugger expressions")
	default:
		return fmt.Errorf("%s is not part of the side-effect-free debugger subset", expressionLabel(expr))
	}
}

func evaluateDebugExpression(parsed *parsedDebugExpression, frame *vmFrame) (runtimeValueInternal, error) {
	if parsed == nil || parsed.expr == nil || frame == nil {
		return voidValue(), fmt.Errorf("no paused frame is available")
	}
	budget := maxDebugExpressionNodes
	return evaluateDebugNode(parsed.expr, frame, &budget)
}

func evaluateDebugNode(expr Expr, frame *vmFrame, budget *int) (runtimeValueInternal, error) {
	*budget--
	if *budget < 0 {
		return voidValue(), fmt.Errorf("expression evaluation limit exceeded")
	}
	switch value := expr.(type) {
	case *IdentifierExpr:
		switch value.Name {
		case "self":
			return frame.self, nil
		case "void":
			return voidValue(), nil
		}
		if result, ok := frame.lookup(value.Name); ok {
			return result, nil
		}
		if frame.self.kind == valueObject && frame.self.o != nil {
			if result, ok := frame.self.o.fields[value.Name]; ok {
				return result, nil
			}
		}
		return voidValue(), fmt.Errorf("unknown local, formal, or attribute %q", value.Name)
	case *IntExpr:
		return intValue(value.Value), nil
	case *StringExpr:
		return stringValue(value.Value), nil
	case *BoolExpr:
		return boolValue(value.Value), nil
	case *IsVoidExpr:
		operand, err := evaluateDebugNode(value.Value, frame, budget)
		if err != nil {
			return voidValue(), err
		}
		return boolValue(operand.isVoid()), nil
	case *UnaryExpr:
		operand, err := evaluateDebugNode(value.Value, frame, budget)
		if err != nil {
			return voidValue(), err
		}
		switch value.Operator {
		case TokenNot:
			if operand.kind != valueBool {
				return voidValue(), fmt.Errorf("not expects Bool, received %s", operand.typeName())
			}
			return boolValue(!operand.b), nil
		case TokenTilde:
			if operand.kind != valueInt {
				return voidValue(), fmt.Errorf("~ expects Int, received %s", operand.typeName())
			}
			return intValue(operand.i.complement()), nil
		}
	case *BinaryExpr:
		left, err := evaluateDebugNode(value.Left, frame, budget)
		if err != nil {
			return voidValue(), err
		}
		right, err := evaluateDebugNode(value.Right, frame, budget)
		if err != nil {
			return voidValue(), err
		}
		if value.Operator == TokenEqual {
			return boolValue(debugValuesEqual(left, right)), nil
		}
		if left.kind != valueInt || right.kind != valueInt {
			return voidValue(), fmt.Errorf("%s expects Int operands, received %s and %s", value.Operator, left.typeName(), right.typeName())
		}
		switch value.Operator {
		case TokenPlus:
			return intValue(left.i.add(right.i)), nil
		case TokenMinus:
			return intValue(left.i.subtract(right.i)), nil
		case TokenStar:
			return intValue(left.i.multiply(right.i)), nil
		case TokenSlash:
			result, ok := left.i.divide(right.i)
			if !ok {
				return voidValue(), fmt.Errorf("division by zero")
			}
			return intValue(result), nil
		case TokenLess:
			return boolValue(left.i.less(right.i)), nil
		case TokenLessEqual:
			return boolValue(left.i.lessEqual(right.i)), nil
		}
	}
	return voidValue(), fmt.Errorf("unsupported debugger expression")
}

func debugValuesEqual(left, right runtimeValueInternal) bool {
	if left.isVoid() || right.isVoid() {
		return left.isVoid() && right.isVoid()
	}
	if left.kind != right.kind {
		return false
	}
	switch left.kind {
	case valueInt:
		return left.i.equal(right.i)
	case valueString:
		return left.s == right.s
	case valueBool:
		return left.b == right.b
	case valueObject:
		return left.o == right.o
	}
	return false
}
