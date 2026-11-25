// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"strings"
)

func (a *analyzer) unusedBinding(name string, span Span) {
	a.diagnostics = append(a.diagnostics, warningDiagnostic("semantic", "COL3180", fmt.Sprintf("binding %s is never read", name), "Review whether the binding is needed; its initializer and assignments still execute.", span))
}

func (a *analyzer) unreachableBranch(span Span) {
	a.diagnostics = append(a.diagnostics, warningDiagnostic("semantic", "COL3181", "branch is unreachable because its condition is a Boolean literal", "The branch is still type-checked; this warning does not change execution.", span))
}

func (a *analyzer) selfAssignment(expression *AssignExpr) {
	d := warningDiagnostic("semantic", "COL3182", fmt.Sprintf("assignment of %s to itself preserves its current value", expression.Name), "Replace the assignment with the identifier to keep the expression's value.", expression.GetSpan())
	d.QuickFix = []QuickFix{{Title: "Replace self-assignment with its value", Replacement: expression.Name, Span: expression.GetSpan()}}
	a.diagnostics = append(a.diagnostics, d)
}

func (a *analyzer) redundantIsvoid(span Span) {
	a.diagnostics = append(a.diagnostics, warningDiagnostic("semantic", "COL3183", "isvoid is false if this literal or allocation evaluates successfully", "Evaluation is still required: initialization, failures, and execution limits are preserved. No automatic replacement is offered.", span))
}

// ValidateWarningRules accepts all (also the empty default), none, or an
// explicit comma-separated selection of stable diagnostic codes.
func ValidateWarningRules(rules string) error {
	if rules == "" || rules == "all" || rules == "none" {
		return nil
	}
	if len(rules) > 256 {
		return fmt.Errorf("warning rule selection exceeds 256 bytes")
	}
	for _, code := range strings.Split(rules, ",") {
		switch code {
		case "COL3161", "COL3180", "COL3181", "COL3182", "COL3183":
		default:
			return fmt.Errorf("unknown warning rule %q", code)
		}
	}
	return nil
}

func selectWarnings(diagnostics []Diagnostic, rules string) []Diagnostic {
	if rules == "" || rules == "all" {
		return diagnostics
	}
	selected := "," + rules + ","
	result := make([]Diagnostic, 0, len(diagnostics))
	for _, d := range diagnostics {
		if d.Severity != SeverityWarning || strings.Contains(selected, ","+d.Code+",") {
			result = append(result, d)
		}
	}
	return result
}
