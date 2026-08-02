// Package ocpp implements OCPP 1.6J (JSON-over-WebSocket) message framing.
//
// Frames are JSON arrays:
//
//	CALL       [2, "<uid>", "<Action>", {payload}]
//	CALLRESULT [3, "<uid>", {payload}]
//	CALLERROR  [4, "<uid>", "<errorCode>", "<errorDescription>", {details}]
//
// Message IDs are treated as opaque strings and never rewritten: the Easee uses
// short numeric IDs while a CSMS uses UUIDs, and both must relay untouched (see
// REQUIREMENTS Appendix A, FR-20).
package ocpp

import (
	"encoding/json"
	"fmt"
)

// Subprotocol is the WebSocket subprotocol negotiated for OCPP 1.6 (NFR-6).
const Subprotocol = "ocpp1.6"

// MessageType is the OCPP-J message type id (first array element).
type MessageType int

const (
	CALL       MessageType = 2
	CALLRESULT MessageType = 3
	CALLERROR  MessageType = 4
)

func (t MessageType) String() string {
	switch t {
	case CALL:
		return "CALL"
	case CALLRESULT:
		return "CALLRESULT"
	case CALLERROR:
		return "CALLERROR"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", int(t))
	}
}

// Frame is a parsed OCPP-J frame. Only the fields relevant to its Type are set.
// Raw holds the original bytes so the frame can be relayed byte-for-byte.
type Frame struct {
	Type         MessageType
	UniqueID     string
	Action       string          // CALL only
	Payload      json.RawMessage // CALL and CALLRESULT
	ErrorCode    string          // CALLERROR
	ErrorDesc    string          // CALLERROR
	ErrorDetails json.RawMessage // CALLERROR
	Raw          json.RawMessage // original message bytes
}

// Parse decodes a single OCPP-J frame.
func Parse(data []byte) (*Frame, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, fmt.Errorf("ocpp: not a JSON array: %w", err)
	}
	if len(arr) < 2 {
		return nil, fmt.Errorf("ocpp: frame has %d elements, need at least 2", len(arr))
	}

	var mt int
	if err := json.Unmarshal(arr[0], &mt); err != nil {
		return nil, fmt.Errorf("ocpp: bad message type: %w", err)
	}

	f := &Frame{Type: MessageType(mt), Raw: append(json.RawMessage(nil), data...)}
	if err := json.Unmarshal(arr[1], &f.UniqueID); err != nil {
		return nil, fmt.Errorf("ocpp: bad unique id: %w", err)
	}

	switch f.Type {
	case CALL:
		if len(arr) < 4 {
			return nil, fmt.Errorf("ocpp: CALL needs 4 elements, got %d", len(arr))
		}
		if err := json.Unmarshal(arr[2], &f.Action); err != nil {
			return nil, fmt.Errorf("ocpp: bad action: %w", err)
		}
		f.Payload = arr[3]
	case CALLRESULT:
		if len(arr) < 3 {
			return nil, fmt.Errorf("ocpp: CALLRESULT needs 3 elements, got %d", len(arr))
		}
		f.Payload = arr[2]
	case CALLERROR:
		if len(arr) < 5 {
			return nil, fmt.Errorf("ocpp: CALLERROR needs 5 elements, got %d", len(arr))
		}
		_ = json.Unmarshal(arr[2], &f.ErrorCode)
		_ = json.Unmarshal(arr[3], &f.ErrorDesc)
		f.ErrorDetails = arr[4]
	default:
		return nil, fmt.Errorf("ocpp: unknown message type %d", mt)
	}
	return f, nil
}

// Call builds a CALL frame.
func Call(uniqueID, action string, payload any) ([]byte, error) {
	if payload == nil {
		payload = struct{}{}
	}
	return json.Marshal([]any{int(CALL), uniqueID, action, payload})
}

// Result builds a CALLRESULT frame in response to uniqueID.
func Result(uniqueID string, payload any) ([]byte, error) {
	if payload == nil {
		payload = struct{}{}
	}
	return json.Marshal([]any{int(CALLRESULT), uniqueID, payload})
}

// CallError builds a CALLERROR frame in response to uniqueID.
func CallError(uniqueID, code, desc string, details any) ([]byte, error) {
	if details == nil {
		details = struct{}{}
	}
	return json.Marshal([]any{int(CALLERROR), uniqueID, code, desc, details})
}
