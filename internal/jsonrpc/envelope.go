package jsonrpc

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Method names cassette cares about. Everything else is routed by kind alone.
const (
	MethodToolsCall = "tools/call"
	MethodCancelled = "notifications/cancelled"
)

// Kind classifies a message by JSON-RPC shape.
type Kind uint8

const (
	KindUnknown Kind = iota

	// KindRequest has both a method and an id, so a response is expected.
	KindRequest

	// KindNotification has a method and no id. Nothing is expected back.
	KindNotification

	// KindResponse has an id and no method. It carries either result or
	// error and correlates to an earlier request.
	KindResponse
)

func (k Kind) String() string {
	switch k {
	case KindRequest:
		return "request"
	case KindNotification:
		return "notification"
	case KindResponse:
		return "response"
	default:
		return "unknown"
	}
}

// Envelope is the routing information in a message, and nothing more.
//
// What is absent here is the design. There is no Result and no Params field,
// because cassette never needs to understand a tool's response in order to
// move it: it is handed to the agent unchanged. Keeping payloads out of this
// struct is what lets a 500 KB file read pass through without being
// materialized into Go values.
type Envelope struct {
	Kind Kind

	// ID is the raw JSON of the id, either a number or a string, exactly as
	// it appeared. It is kept raw rather than decoded because correlation
	// needs to survive a round trip byte for byte, and because a JSON number
	// decoded into float64 and re-encoded is not always the same text.
	ID []byte

	// Method is empty on responses.
	Method string

	// IsError reports that a response carried an error object.
	IsError bool
}

// HasID reports whether the message carried a usable id. A literal null id,
// which JSON-RPC uses for a response to an unparseable request, counts as
// absent.
func (e Envelope) HasID() bool {
	return len(e.ID) > 0 && !bytes.Equal(e.ID, []byte("null"))
}

// header is stage one of parsing.
//
// It omits params and result on purpose. encoding/json walks fields that have
// no counterpart in the target struct and discards them without building any
// Go value, so a response carrying a large tool result is validated but never
// materialized. The alternative, a json.RawMessage field for result, would
// copy the entire payload on every single message.
type header struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Error  json.RawMessage `json:"error"`
}

// ParseEnvelope extracts routing information from a message.
//
// Callers must treat a parse failure as non-fatal. A proxy that drops or
// rejects anything it cannot understand is a proxy that breaks agents on
// protocol revisions it has never seen, which defeats the purpose of being
// transparent. Forward first, classify second.
func ParseEnvelope(msg []byte) (Envelope, error) {
	var h header
	if err := json.Unmarshal(msg, &h); err != nil {
		return Envelope{}, fmt.Errorf("jsonrpc: parse envelope: %w", err)
	}

	env := Envelope{
		Method:  h.Method,
		IsError: len(h.Error) > 0,
	}
	if len(h.ID) > 0 && !bytes.Equal(h.ID, []byte("null")) {
		env.ID = h.ID
	}

	switch {
	case env.Method != "" && env.ID != nil:
		env.Kind = KindRequest
	case env.Method != "":
		env.Kind = KindNotification
	case env.ID != nil || env.IsError:
		env.Kind = KindResponse
	}
	return env, nil
}

// IsToolCall reports whether this envelope is a tools/call request, the only
// message type cassette records or serves from a tape.
func (e Envelope) IsToolCall() bool {
	return e.Kind == KindRequest && e.Method == MethodToolsCall
}

// paramsMessage extracts only params, again omitting everything else so a
// large sibling field is skipped rather than copied.
type paramsMessage struct {
	Params json.RawMessage `json:"params"`
}

// ParseParams returns the raw params of a request or notification.
//
// Matching needs this for every method, not just tools/call: during replay
// there is no server, so initialize and tools/list have to be answered off
// the tape too, and they are distinguished by their params.
//
// Returns nil when there are no params, which is a legitimate shape and not
// an error.
func ParseParams(msg []byte) (json.RawMessage, error) {
	var m paramsMessage
	if err := json.Unmarshal(msg, &m); err != nil {
		return nil, fmt.Errorf("jsonrpc: parse params: %w", err)
	}
	return m.Params, nil
}

// ToolCall is the part of a tools/call request that identifies what was
// asked for.
type ToolCall struct {
	Name string

	// Arguments is the raw JSON of params.arguments. It stays raw because it
	// is about to be hashed and matched against a tape, and re-encoding a
	// decoded map would reorder keys and change the hash.
	Arguments json.RawMessage
}

type toolCallMessage struct {
	Params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"params"`
}

// ParseToolCall extracts the tool name and arguments from a tools/call
// request. This is the second parse of the same bytes, and that is a
// deliberate trade: requests are small, responses are not, and paying twice
// on the small half avoids paying once on the large half.
func ParseToolCall(msg []byte) (ToolCall, error) {
	var m toolCallMessage
	if err := json.Unmarshal(msg, &m); err != nil {
		return ToolCall{}, fmt.Errorf("jsonrpc: parse tools/call: %w", err)
	}
	if m.Params.Name == "" {
		return ToolCall{}, fmt.Errorf("jsonrpc: tools/call missing params.name")
	}
	return ToolCall{Name: m.Params.Name, Arguments: m.Params.Arguments}, nil
}
