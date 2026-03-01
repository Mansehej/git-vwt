package vwt

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"time"
)

func GenerateID(now time.Time) (string, error) {
	// Sortable and human-friendly, with randomness to avoid collisions.
	// Example: 20260223-123456-1a2b3c4d
	var b [4]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", now.UTC().Format("20060102-150405"), hex.EncodeToString(b[:])), nil
}
