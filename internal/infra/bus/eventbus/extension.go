package eventbus

import (
	"fmt"

	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/domain/errs"
	"github.com/coachpo/meltica/internal/domain/schema"
)

func enforceExtensionPayloadCap(evt *schema.Event, capBytes int) error {
	if evt == nil || evt.Type != schema.ExtensionEventType {
		return nil
	}
	if capBytes <= 0 {
		return nil
	}
	size, err := extensionPayloadSize(evt.Payload)
	if err != nil {
		return fmt.Errorf("eventbus extension payload encode: %w", err)
	}
	if size > capBytes {
		return errs.New(
			"eventbus/extension",
			errs.CodeInvalid,
			errs.WithMessage(fmt.Sprintf("extension payload %d bytes exceeds cap %d bytes", size, capBytes)),
		)
	}
	return nil
}

func extensionPayloadSize(payload any) (int, error) {
	switch v := payload.(type) {
	case nil:
		return 0, nil
	case []byte:
		return len(v), nil
	case json.RawMessage:
		return len(v), nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return 0, err
		}
		return len(data), nil
	}
}
