// Package blueprint decodes and re-encodes Factorio blueprint exchange
// strings. An exchange string is a single version marker byte ('0' today)
// followed by base64-encoded, zlib-compressed JSON.
package blueprint

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"factorio-expo/apps/expo-api/internal/fault"
)

// versionMarker is the only exchange-string format version Factorio currently
// emits. Anything else is rejected rather than guessed at.
const versionMarker = '0'

// ErrInvalidExchangeString marks any input that is not a decodable Factorio
// exchange string.
var ErrInvalidExchangeString = fault.New(fault.Unprocessable, "invalid_exchange_string", "That does not look like a Factorio blueprint string. Copy it again from the game.")

// Decode turns an exchange string into its raw JSON payload, enforcing the
// version marker and a decompressed byte ceiling so a small input cannot
// expand into an unbounded allocation.
func Decode(exchange string, maxDecodedBytes int64) (json.RawMessage, error) {
	if len(exchange) < 2 || exchange[0] != versionMarker {
		return nil, ErrInvalidExchangeString
	}

	compressed, err := base64.StdEncoding.DecodeString(exchange[1:])
	if err != nil {
		return nil, ErrInvalidExchangeString
	}

	reader, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, ErrInvalidExchangeString
	}
	defer reader.Close()

	// Read one byte past the limit so a payload exactly at the limit is
	// accepted while an oversized one stays detectable.
	payload, err := io.ReadAll(io.LimitReader(reader, maxDecodedBytes+1))
	if err != nil {
		return nil, ErrInvalidExchangeString
	}
	if int64(len(payload)) > maxDecodedBytes {
		return nil, ErrInvalidExchangeString
	}
	if !json.Valid(payload) {
		return nil, ErrInvalidExchangeString
	}
	return payload, nil
}

// Encode compresses a decoded payload back into an exchange string. This is
// how self-contained leaf strings are rebuilt for blueprints nested in books.
func Encode(payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode blueprint payload: %w", err)
	}

	var compressed bytes.Buffer
	writer, err := zlib.NewWriterLevel(&compressed, zlib.BestCompression)
	if err != nil {
		return "", fmt.Errorf("create compressor: %w", err)
	}
	if _, err := writer.Write(raw); err != nil {
		return "", fmt.Errorf("compress blueprint payload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("finish compression: %w", err)
	}

	return string(rune(versionMarker)) + base64.StdEncoding.EncodeToString(compressed.Bytes()), nil
}
