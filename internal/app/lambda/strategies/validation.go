package strategies

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/coachpo/meltica/internal/domain/schema"
)

// MetadataIssue represents a single validation failure within metadata.
type MetadataIssue struct {
	Path    string
	Message string
}

// ValidateMetadata ensures the supplied metadata includes required fields.
func ValidateMetadata(meta Metadata) []MetadataIssue {
	var issues []MetadataIssue

	if name := strings.TrimSpace(meta.Name); name == "" {
		issues = append(issues, MetadataIssue{
			Path:    "metadata.name",
			Message: "name required",
		})
	}

	displayName := strings.TrimSpace(meta.DisplayName)
	switch length := utf8.RuneCountInString(displayName); {
	case length == 0:
		issues = append(issues, MetadataIssue{
			Path:    "metadata.displayName",
			Message: "displayName required",
		})
	case length > 80:
		issues = append(issues, MetadataIssue{
			Path:    "metadata.displayName",
			Message: "displayName must be 80 characters or fewer",
		})
	}

	if len(meta.Events) == 0 {
		issues = append(issues, MetadataIssue{
			Path:    "metadata.events",
			Message: "events must include at least one event type",
		})
	} else {
		for idx, raw := range meta.Events {
			if !isValidEventType(raw) {
				issues = append(issues, MetadataIssue{
					Path:    fmt.Sprintf("metadata.events[%d]", idx),
					Message: fmt.Sprintf("unsupported event type %q", raw),
				})
			}
		}
	}

	for idx, cfg := range meta.Config {
		name := strings.TrimSpace(cfg.Name)
		if name == "" {
			issues = append(issues, MetadataIssue{
				Path:    fmt.Sprintf("metadata.config[%d].name", idx),
				Message: "name required",
			})
		}
		fieldType := strings.TrimSpace(cfg.Type)
		if fieldType == "" {
			issues = append(issues, MetadataIssue{
				Path:    fmt.Sprintf("metadata.config[%d].type", idx),
				Message: "type required",
			})
		}
	}

	return issues
}

func isValidEventType(evt schema.EventType) bool {
	trimmed := schema.EventType(strings.TrimSpace(string(evt)))
	switch trimmed {
	case schema.EventTypeBookSnapshot,
		schema.EventTypeTrade,
		schema.EventTypeTicker,
		schema.EventTypeExecReport,
		schema.EventTypeKlineSummary,
		schema.EventTypeInstrumentUpdate,
		schema.EventTypeBalanceUpdate,
		schema.EventTypeRiskControl,
		schema.ExtensionEventType:
		return true
	default:
		return false
	}
}
