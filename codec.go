package rostam

import (
	"encoding"
	"encoding/binary"
	"math"
	"net"
	"strconv"
	"time"
)

// Stored value layout. The server's incr_ex adds to a value only when it is
// exactly 8 bytes (a big-endian int64), so integers are stored bare and every
// other value goes behind a one-byte header. A headered value must never total
// 8 bytes either, or a 7-byte string would read back as a number: those get
// the padded header and a trailing zero byte instead.
const (
	counterSize = 8

	headerValue  byte = 0x00 // 0x00 + payload
	headerPadded byte = 0x01 // 0x01 + payload + 0x00, when 0x00 + payload would be 8 bytes
)

// encodeValue turns a cache value into its stored bytes. Integers become
// counters; everything else is stored as the string go-redis would have
// written, so Get returns the same strings goravel/redis does.
func encodeValue(value any) ([]byte, error) {
	if n, ok := asCounter(value); ok {
		return encodeCounter(n), nil
	}

	payload, err := stringPayload(value)
	if err != nil {
		return nil, err
	}

	if 1+len(payload) == counterSize {
		out := make([]byte, 0, len(payload)+2)
		out = append(out, headerPadded)
		out = append(out, payload...)

		return append(out, 0x00), nil
	}

	out := make([]byte, 0, len(payload)+1)
	out = append(out, headerValue)

	return append(out, payload...), nil
}

// decodeValue reverses encodeValue. A counter reads back as its decimal
// string, the way Redis returns an INCR'd key.
func decodeValue(stored []byte) (string, error) {
	if len(stored) == counterSize {
		return strconv.FormatInt(decodeCounter(stored), 10), nil
	}

	if len(stored) == 0 {
		return "", ErrCorruptValue
	}

	switch stored[0] {
	case headerValue:
		return string(stored[1:]), nil
	case headerPadded:
		if len(stored) < 2 || stored[len(stored)-1] != 0x00 {
			return "", ErrCorruptValue
		}

		return string(stored[1 : len(stored)-1]), nil
	default:
		return "", ErrCorruptValue
	}
}

func encodeCounter(n int64) []byte {
	out := make([]byte, counterSize)
	binary.BigEndian.PutUint64(out, uint64(n)) //nolint:gosec // two's complement round-trips through decodeCounter

	return out
}

func decodeCounter(stored []byte) int64 {
	return int64(binary.BigEndian.Uint64(stored)) //nolint:gosec // two's complement round-trips through encodeCounter
}

// asCounter reports whether value is an integer that fits an int64, which is
// what the server can increment. time.Duration counts: go-redis writes it as
// its nanoseconds.
func asCounter(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint:
		if uint64(v) > math.MaxInt64 {
			return 0, false
		}

		return int64(v), true
	case uint64:
		if v > math.MaxInt64 {
			return 0, false
		}

		return int64(v), true
	case time.Duration:
		return int64(v), true
	default:
		return 0, false
	}
}

// stringPayload mirrors go-redis's argument encoding for the types it
// accepts, and refuses the rest the same way.
func stringPayload(value any) ([]byte, error) {
	switch v := value.(type) {
	case nil:
		return []byte{}, nil
	case string:
		return []byte(v), nil
	case []byte:
		return v, nil
	case uint:
		return strconv.AppendUint(nil, uint64(v), 10), nil
	case uint64:
		return strconv.AppendUint(nil, v, 10), nil
	case float32:
		return strconv.AppendFloat(nil, float64(v), 'f', -1, 32), nil
	case float64:
		return strconv.AppendFloat(nil, v, 'f', -1, 64), nil
	case bool:
		if v {
			return []byte("1"), nil
		}

		return []byte("0"), nil
	case time.Time:
		return v.AppendFormat(nil, time.RFC3339Nano), nil
	case net.IP:
		return []byte(v), nil
	case encoding.BinaryMarshaler:
		return v.MarshalBinary()
	default:
		return nil, ErrUnsupportedValue.Args(value)
	}
}
