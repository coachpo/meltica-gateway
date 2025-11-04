package js

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/dop251/goja"

	"github.com/coachpo/meltica/internal/app/lambda/strategies"
)

// DiagnosticStage enumerates validation stages for strategy modules.
type DiagnosticStage string

const (
	// DiagnosticStageCompile captures syntax and parsing failures.
	DiagnosticStageCompile DiagnosticStage = "compile"
	// DiagnosticStageExecute captures runtime errors triggered during module evaluation.
	DiagnosticStageExecute DiagnosticStage = "execute"
	// DiagnosticStageValidation captures metadata validation issues.
	DiagnosticStageValidation DiagnosticStage = "validation"
)

// Diagnostic describes a single validation finding that should be returned to clients.
type Diagnostic struct {
	Stage   DiagnosticStage `json:"stage"`
	Message string          `json:"message"`
	Line    int             `json:"line,omitempty"`
	Column  int             `json:"column,omitempty"`
	Hint    string          `json:"hint,omitempty"`
}

// DiagnosticError aggregates diagnostics for downstream handlers.
type DiagnosticError struct {
	message     string
	diagnostics []Diagnostic
	cause       error
}

// NewDiagnosticError constructs a diagnostic error with an optional message and cause.
func NewDiagnosticError(message string, cause error, diagnostics ...Diagnostic) *DiagnosticError {
	return &DiagnosticError{
		message:     strings.TrimSpace(message),
		diagnostics: append([]Diagnostic(nil), diagnostics...),
		cause:       cause,
	}
}

// Error implements the error interface.
func (e *DiagnosticError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.message != "" {
		return e.message
	}
	if len(e.diagnostics) > 0 {
		return e.diagnostics[0].Message
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	return "strategy module diagnostics"
}

// Unwrap returns the underlying cause.
func (e *DiagnosticError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Diagnostics returns a copy of the aggregated diagnostics.
func (e *DiagnosticError) Diagnostics() []Diagnostic {
	if e == nil || len(e.diagnostics) == 0 {
		return nil
	}
	out := make([]Diagnostic, len(e.diagnostics))
	copy(out, e.diagnostics)
	return out
}

// Append adds diagnostics to the aggregation.
func (e *DiagnosticError) Append(diags ...Diagnostic) {
	if e == nil || len(diags) == 0 {
		return
	}
	for _, diag := range diags {
		if diag.Stage == "" {
			diag.Stage = DiagnosticStageValidation
		}
		diag.Message = strings.TrimSpace(diag.Message)
		if diag.Message == "" {
			continue
		}
		e.diagnostics = append(e.diagnostics, diag)
	}
}

// compileDiagnostic converts compilation failures into diagnostics.
func compileDiagnostic(err error) Diagnostic {
	msg := diagnosticMessage(err)
	diag := Diagnostic{
		Stage:   DiagnosticStageCompile,
		Message: msg,
		Line:    0,
		Column:  0,
		Hint:    "Fix the JavaScript syntax near the reported location.",
	}
	var syntaxErr *goja.CompilerSyntaxError
	if errors.As(err, &syntaxErr) && syntaxErr != nil {
		if trimmed := strings.TrimSpace(syntaxErr.Message); trimmed != "" {
			diag.Message = trimmed
		}
		if syntaxErr.File != nil {
			pos := syntaxErr.File.Position(syntaxErr.Offset)
			if pos.Line > 0 {
				diag.Line = pos.Line
			}
			if pos.Column > 0 {
				diag.Column = pos.Column
			}
		}
	}
	return diag
}

// executeDiagnostic converts runtime evaluation failures into diagnostics.
func executeDiagnostic(err error) Diagnostic {
	diag := Diagnostic{
		Stage:   DiagnosticStageExecute,
		Message: diagnosticMessage(err),
		Line:    0,
		Column:  0,
		Hint:    "Check module initialization and ensure metadata export executes without throwing.",
	}
	var jsErr *goja.Exception
	if errors.As(err, &jsErr) && jsErr != nil {
		if val := jsErr.Value(); !goja.IsUndefined(val) && !goja.IsNull(val) {
			if msg := strings.TrimSpace(val.String()); msg != "" {
				diag.Message = msg
			}
		}
		stack := jsErr.Stack()
		if len(stack) > 0 {
			pos := stack[0].Position()
			if pos.Line > 0 {
				diag.Line = pos.Line
			}
			if pos.Column > 0 {
				diag.Column = pos.Column
			}
		}
	}
	return diag
}

func diagnosticMessage(err error) string {
	if err == nil {
		return "unknown error"
	}
	return pruneStackSuffix(strings.TrimSpace(err.Error()))
}

func pruneStackSuffix(msg string) string {
	const frameSep = "\n"
	if idx := strings.Index(msg, frameSep); idx > 0 {
		msg = msg[:idx]
	}
	if idx := strings.Index(msg, " at "); idx > 0 && idx < len(msg)-4 {
		after := msg[idx+4:]
		if !strings.Contains(after, " at ") {
			msg = msg[:idx]
		}
	}
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "unknown error"
	}
	const maxLen = 256
	if utf8.RuneCountInString(msg) > maxLen {
		return string([]rune(msg)[:maxLen])
	}
	return msg
}

func validationDiagnosticsFromIssues(issues []strategies.MetadataIssue) []Diagnostic {
	if len(issues) == 0 {
		return nil
	}
	out := make([]Diagnostic, 0, len(issues))
	for _, issue := range issues {
		message := strings.TrimSpace(issue.Message)
		if message == "" {
			continue
		}
		hint := strings.TrimSpace(issue.Path)
		diag := Diagnostic{
			Stage:   DiagnosticStageValidation,
			Message: message,
			Line:    0,
			Column:  0,
			Hint:    hint,
		}
		out = append(out, diag)
	}
	return out
}

// AsDiagnosticError attempts to convert an error into a DiagnosticError.
func AsDiagnosticError(err error) (*DiagnosticError, bool) {
	if err == nil {
		return nil, false
	}
	var diagErr *DiagnosticError
	if errors.As(err, &diagErr) {
		return diagErr, true
	}
	return nil, false
}
