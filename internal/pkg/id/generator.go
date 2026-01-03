package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/google/uuid"
)

func RequestID() string { return "agent-" + uuid.New().String() }

func SessionID() string {
	max := new(big.Int).SetUint64(9e18)
	n, _ := rand.Int(rand.Reader, max)
	return "-" + n.String()
}

func ProjectID() string {
	adjectives := []string{"useful", "bright", "swift", "calm", "bold", "happy", "clever", "gentle", "quick", "brave"}
	nouns := []string{"fuze", "wave", "spark", "flow", "core", "beam", "star", "wind", "leaf", "cloud"}

	adj := randIndex(adjectives)
	noun := randIndex(nouns)
	suffix := randomAlphanumeric(5)

	return fmt.Sprintf("%s-%s-%s", adj, noun, suffix)
}

func ToolCallID() string {
	id := uuid.New().String()
	return "call_" + strings.ReplaceAll(id, "-", "")
}

func SecureToken(length int) string {
	bytes := make([]byte, length)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func ChatCompletionID() string { return fmt.Sprintf("chatcmpl-%s", uuid.New().String()[:8]) }

func randIndex(list []string) string {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	return list[int(n.Int64())]
}

func randomAlphanumeric(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		result[i] = charset[int(n.Int64())]
	}
	return string(result)
}

