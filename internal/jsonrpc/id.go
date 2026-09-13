package jsonrpc

// ReplaceID rewrites the top-level "id" value of a message, leaving every
// other byte exactly as it was.
//
// Replay needs this because the agent does not reuse its JSON-RPC ids across
// runs. The recorded response says id 7; the live request that it answers
// might be id 42. Sending the recorded bytes unchanged would leave the agent
// waiting forever for a reply it can correlate.
//
// The alternative designs were both worse. Normalizing ids to a fixed width
// at record time would allow an in-place patch with no allocation, but the
// tape would no longer hold the literal bytes the server sent, which is the
// strongest claim this project makes. Storing an id offset per entry would
// avoid most copies but adds a field to every index entry and real
// complexity to writer and reader for a saving of roughly 200 nanoseconds
// against a model turn measured in seconds.
//
// So this splices, and the tape keeps exactly what crossed the wire.
//
// A hand-written scan rather than decode-and-re-encode, because encoding/json
// would reorder keys and reformat whitespace, and preserving those is the
// whole point. Returns the original slice and false when there is no
// top-level id, which is the normal shape of a notification.
func ReplaceID(msg, newID []byte) ([]byte, bool) {
	start, end, ok := findTopLevelID(msg)
	if !ok {
		return msg, false
	}

	out := make([]byte, 0, len(msg)-(end-start)+len(newID))
	out = append(out, msg[:start]...)
	out = append(out, newID...)
	out = append(out, msg[end:]...)
	return out, true
}

// findTopLevelID returns the byte span of the top-level id's value.
//
// Only depth 1 is considered. A nested "id" inside params or result is
// someone else's field and must not be touched: tool arguments routinely
// carry an id of their own, and rewriting one would corrupt the payload.
func findTopLevelID(msg []byte) (start, end int, ok bool) {
	i := skipSpace(msg, 0)
	if i >= len(msg) || msg[i] != '{' {
		return 0, 0, false
	}
	i++

	for {
		i = skipSpace(msg, i)
		if i >= len(msg) || msg[i] == '}' {
			return 0, 0, false
		}
		if msg[i] == ',' {
			i++
			continue
		}
		if msg[i] != '"' {
			return 0, 0, false
		}

		keyStart := i
		keyEnd, ok := scanString(msg, i)
		if !ok {
			return 0, 0, false
		}
		isID := keyEnd-keyStart == 4 && string(msg[keyStart:keyEnd]) == `"id"`

		i = skipSpace(msg, keyEnd)
		if i >= len(msg) || msg[i] != ':' {
			return 0, 0, false
		}
		i = skipSpace(msg, i+1)

		valStart := i
		valEnd, ok := scanValue(msg, i)
		if !ok {
			return 0, 0, false
		}
		if isID {
			return valStart, valEnd, true
		}
		i = valEnd
	}
}

func skipSpace(b []byte, i int) int {
	for i < len(b) {
		switch b[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

// scanString returns the index just past a string token, handling escapes.
// A backslash always consumes the next byte, which is enough: the only
// escape that could otherwise hide a closing quote is \" itself.
func scanString(b []byte, i int) (int, bool) {
	if i >= len(b) || b[i] != '"' {
		return 0, false
	}
	for i++; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i + 1, true
		}
	}
	return 0, false
}

// scanValue returns the index just past any JSON value.
func scanValue(b []byte, i int) (int, bool) {
	if i >= len(b) {
		return 0, false
	}
	switch b[i] {
	case '"':
		return scanString(b, i)
	case '{', '[':
		return scanContainer(b, i)
	default:
		// Number, true, false or null. All of them end at the first
		// structural character or whitespace. An empty span means the
		// value was missing, as in {"id":}, which is malformed rather
		// than a zero-length literal.
		start := i
	scan:
		for ; i < len(b); i++ {
			switch b[i] {
			case ',', '}', ']', ' ', '\t', '\n', '\r':
				break scan
			}
		}
		if i == start {
			return 0, false
		}
		return i, true
	}
}

// scanContainer walks a balanced object or array, ignoring structural
// characters that appear inside strings.
func scanContainer(b []byte, i int) (int, bool) {
	depth := 0
	for i < len(b) {
		switch b[i] {
		case '"':
			next, ok := scanString(b, i)
			if !ok {
				return 0, false
			}
			i = next
			continue
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
		i++
	}
	return 0, false
}
