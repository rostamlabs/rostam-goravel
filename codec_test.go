package rostam

import (
	"errors"
	"math"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type binaryValue struct{ text string }

func (v binaryValue) MarshalBinary() ([]byte, error) { return []byte(v.text), nil }

func TestEncodeValueRoundTripsAsGoRedisStrings(t *testing.T) {
	moment := time.Date(2026, 10, 28, 9, 30, 0, 123, time.UTC)

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"string", "Goravel", "Goravel"},
		{"empty string", "", ""},
		{"bytes", []byte("raw"), "raw"},
		{"nil", nil, ""},
		{"int", 42, "42"},
		{"negative int64", int64(-7), "-7"},
		{"int8", int8(-128), "-128"},
		{"uint32", uint32(4000000000), "4000000000"},
		{"uint64 within int64", uint64(math.MaxInt64), "9223372036854775807"},
		{"uint64 past int64", uint64(math.MaxUint64), "18446744073709551615"},
		{"duration", 2 * time.Second, "2000000000"},
		{"float32", float32(1.5), "1.5"},
		{"float64", 3.25, "3.25"},
		{"true", true, "1"},
		{"false", false, "0"},
		{"time", moment, moment.Format(time.RFC3339Nano)},
		{"ip", net.IPv4(1, 2, 3, 4).To4(), string([]byte{1, 2, 3, 4})},
		{"binary marshaler", binaryValue{"marshalled"}, "marshalled"},
		{"seven bytes", "1234567", "1234567"},
		{"eight bytes", "12345678", "12345678"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stored, err := encodeValue(test.value)
			require.NoError(t, err)

			got, err := decodeValue(stored)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestIntegersAreStoredAsBareCounters(t *testing.T) {
	stored, err := encodeValue(5)
	require.NoError(t, err)

	assert.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 5}, stored)
	assert.Equal(t, int64(5), decodeCounter(stored))
}

func TestUint64PastInt64IsNotACounter(t *testing.T) {
	stored, err := encodeValue(uint64(math.MaxUint64))
	require.NoError(t, err)

	assert.NotEqual(t, counterSize, len(stored))
}

func TestNoNonCounterEverEncodesToEightBytes(t *testing.T) {
	for length := 0; length <= 16; length++ {
		payload := make([]byte, length)
		for i := range payload {
			payload[i] = 'x'
		}

		stored, err := encodeValue(string(payload))
		require.NoError(t, err)
		assert.NotEqual(t, counterSize, len(stored), "payload of %d bytes", length)
	}
}

func TestSevenByteValueUsesThePaddedHeader(t *testing.T) {
	stored, err := encodeValue("abcdefg")
	require.NoError(t, err)

	assert.Equal(t, append(append([]byte{headerPadded}, "abcdefg"...), 0x00), stored)
}

func TestPaddedPayloadMayItselfEndInZero(t *testing.T) {
	stored, err := encodeValue([]byte{'a', 'b', 'c', 'd', 'e', 'f', 0x00})
	require.NoError(t, err)

	got, err := decodeValue(stored)
	require.NoError(t, err)
	assert.Equal(t, string([]byte{'a', 'b', 'c', 'd', 'e', 'f', 0x00}), got)
}

func TestDecodeValueRejectsForeignBytes(t *testing.T) {
	for name, stored := range map[string][]byte{
		"empty":                {},
		"unknown header":       {0x07, 'x'},
		"padded without tail":  {headerPadded, 'x', 'y'},
		"padded header alone":  {headerPadded},
		"plain text by others": []byte("hello"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeValue(stored)
			assert.True(t, errors.Is(err, ErrCorruptValue))
		})
	}
}

func TestUnsupportedTypesAreRefused(t *testing.T) {
	_, err := encodeValue(struct{ Name string }{"Goravel"})

	assert.True(t, errors.Is(err, ErrUnsupportedValue))
}

func TestWireTTL(t *testing.T) {
	assert.Equal(t, time.Duration(0), wireTTL(0))
	assert.Equal(t, time.Millisecond, wireTTL(time.Nanosecond))
	assert.Equal(t, time.Millisecond, wireTTL(999*time.Microsecond))
	assert.Equal(t, 2*time.Second, wireTTL(2*time.Second))
}
