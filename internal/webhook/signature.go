package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

const (
	HeaderEventID   = "X-Windforce-Event"
	HeaderDelivery  = "X-Windforce-Delivery"
	HeaderTimestamp = "X-Windforce-Timestamp"
	HeaderSignature = "X-Windforce-Signature"
)

func TimestampValue(at time.Time) string {
	return strconv.FormatInt(at.UTC().Unix(), 10)
}

func Sign(secret string, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func VerifySignature(secret string, timestamp string, body []byte, signature string) bool {
	provided := strings.TrimSpace(signature)
	if !strings.HasPrefix(provided, "v1=") {
		return false
	}
	expected := Sign(secret, timestamp, body)
	return hmac.Equal([]byte(expected), []byte(provided))
}
