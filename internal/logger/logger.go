package logger

import (
	"anti2api-golang/refactor/internal/config"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// LogLevel matches the original anti2api-go project behavior.
type LogLevel int

const (
	LogOff  LogLevel = 0 // basic logs only
	LogLow  LogLevel = 1 // + client request/response
	LogHigh LogLevel = 2 // + backend request/response
)

const (
	ColorReset  = "\x1b[0m"
	ColorGreen  = "\x1b[32m"
	ColorYellow = "\x1b[33m"
	ColorRed    = "\x1b[31m"
	ColorCyan   = "\x1b[36m"
	ColorGray   = "\x1b[90m"
	ColorBlue   = "\x1b[34m"
	ColorPurple = "\x1b[35m"
)

var currentLogLevel LogLevel

func Init() {
	cfg := config.Get()
	currentLogLevel = parseLogLevel(cfg.Debug)
}

func parseLogLevel(debug string) LogLevel {
	switch strings.ToLower(strings.TrimSpace(debug)) {
	case "low":
		return LogLow
	case "high":
		return LogHigh
	default:
		return LogOff
	}
}

func GetLevel() LogLevel {
	return currentLogLevel
}

func IsClientLogEnabled() bool {
	return currentLogLevel >= LogLow
}

func IsBackendLogEnabled() bool {
	return currentLogLevel >= LogHigh
}

func Info(format string, args ...any) {
	timestamp := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%s%s%s %s[info]%s %s\n", ColorGray, timestamp, ColorReset, ColorGreen, ColorReset, msg)
}

func Warn(format string, args ...any) {
	timestamp := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%s%s%s %s[warn]%s %s\n", ColorGray, timestamp, ColorReset, ColorYellow, ColorReset, msg)
}

func Error(format string, args ...any) {
	timestamp := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%s%s%s %s[error]%s %s\n", ColorGray, timestamp, ColorReset, ColorRed, ColorReset, msg)
}

func Debug(format string, args ...any) {
	if currentLogLevel < LogLow {
		return
	}
	timestamp := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%s%s%s %s[debug]%s %s\n", ColorGray, timestamp, ColorReset, ColorBlue, ColorReset, msg)
}

func Request(method, path string, status int, duration time.Duration) {
	statusColor := ColorGreen
	if status >= 500 {
		statusColor = ColorRed
	} else if status >= 400 {
		statusColor = ColorYellow
	}

	fmt.Printf("%s[%s]%s %s %s%d%s %s%dms%s\n",
		ColorCyan, method, ColorReset,
		path,
		statusColor, status, ColorReset,
		ColorGray, duration.Milliseconds(), ColorReset)
}

func ClientRequest(method, path string, rawJSON []byte) {
	if currentLogLevel < LogLow {
		return
	}
	fmt.Printf("%s===================== 客户端请求 ======================%s\n", ColorPurple, ColorReset)
	fmt.Printf("%s[客户端请求]%s %s%s%s %s\n", ColorPurple, ColorReset, ColorCyan, method, ColorReset, path)
	if len(rawJSON) > 0 {
		fmt.Println(formatRawJSON(rawJSON))
	}
	fmt.Printf("%s=========================================================%s\n", ColorPurple, ColorReset)
}

func ClientResponse(status int, duration time.Duration, body any) {
	if currentLogLevel < LogLow {
		return
	}

	statusColor := ColorGreen
	if status >= 400 {
		statusColor = ColorRed
	}

	fmt.Printf("%s===================== 客户端响应 ======================%s\n", ColorPurple, ColorReset)
	fmt.Printf("%s[客户端响应]%s %s%d%s %s%dms%s\n", ColorPurple, ColorReset, statusColor, status, ColorReset, ColorGray, duration.Milliseconds(), ColorReset)
	if body != nil {
		printJSON(body)
	}
	fmt.Printf("%s==========================================================%s\n", ColorPurple, ColorReset)
}

func BackendRequest(method, url string, rawJSON []byte) {
	if currentLogLevel < LogHigh {
		return
	}
	fmt.Printf("%s====================== 后端请求 ========================%s\n", ColorYellow, ColorReset)
	fmt.Printf("%s[后端请求]%s %s%s%s %s\n", ColorYellow, ColorReset, ColorCyan, method, ColorReset, url)
	if len(rawJSON) > 0 {
		fmt.Println(formatRawJSON(rawJSON))
	}
	fmt.Printf("%s==========================================================%s\n", ColorYellow, ColorReset)
}

func ClientRequestWithHeaders(method, path string, headers http.Header, rawJSON []byte) {
	if currentLogLevel < LogLow {
		return
	}
	fmt.Printf("%s===================== 客户端请求 ======================%s\n", ColorPurple, ColorReset)
	fmt.Printf("%s[客户端请求]%s %s%s%s %s\n", ColorPurple, ColorReset, ColorCyan, method, ColorReset, path)
	if headers != nil {
		fmt.Printf("%s[客户端请求头]%s\n", ColorPurple, ColorReset)
		printJSON(redactHeaders(headers))
	}
	if len(rawJSON) > 0 {
		fmt.Println(formatRawJSON(rawJSON))
	}
	fmt.Printf("%s=========================================================%s\n", ColorPurple, ColorReset)
}

func BackendRequestWithHeaders(method, url string, headers http.Header, rawJSON []byte) {
	if currentLogLevel < LogHigh {
		return
	}
	fmt.Printf("%s====================== 后端请求 ========================%s\n", ColorYellow, ColorReset)
	fmt.Printf("%s[后端请求]%s %s%s%s %s\n", ColorYellow, ColorReset, ColorCyan, method, ColorReset, url)
	if headers != nil {
		fmt.Printf("%s[后端请求头]%s\n", ColorYellow, ColorReset)
		printJSON(redactHeaders(headers))
	}
	if len(rawJSON) > 0 {
		fmt.Println(formatRawJSON(rawJSON))
	}
	fmt.Printf("%s==========================================================%s\n", ColorYellow, ColorReset)
}

func redactHeaders(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, v := range h {
		kl := strings.ToLower(k)
		if kl == "authorization" || kl == "proxy-authorization" {
			out[k] = []string{"Bearer ***"}
			continue
		}
		out[k] = append([]string(nil), v...)
	}
	return out
}

func BackendResponse(status int, duration time.Duration, body any) {
	if currentLogLevel < LogHigh {
		return
	}
	statusColor := ColorGreen
	if status >= 400 {
		statusColor = ColorRed
	}
	fmt.Printf("%s====================== 后端响应 ========================%s\n", ColorGreen, ColorReset)
	fmt.Printf("%s[后端响应]%s %s%d%s %s%dms%s\n", ColorGreen, ColorReset, statusColor, status, ColorReset, ColorGray, duration.Milliseconds(), ColorReset)
	if body != nil {
		printJSON(body)
	}
	fmt.Printf("%s==========================================================%s\n", ColorGreen, ColorReset)
}

func BackendStreamResponse(status int, duration time.Duration, body any) {
	if currentLogLevel < LogHigh {
		return
	}
	statusColor := ColorGreen
	if status >= 400 {
		statusColor = ColorRed
	}
	fmt.Printf("%s==================== 后端流式响应 =======================%s\n", ColorGreen, ColorReset)
	fmt.Printf("%s[后端流式]%s %s%d%s %s%dms%s\n", ColorGreen, ColorReset, statusColor, status, ColorReset, ColorGray, duration.Milliseconds(), ColorReset)
	if body != nil {
		printJSON(body)
	}
	fmt.Printf("%s==========================================================%s\n", ColorGreen, ColorReset)
}

func ClientStreamResponse(status int, duration time.Duration, body any) {
	if currentLogLevel < LogLow {
		return
	}
	statusColor := ColorGreen
	if status >= 400 {
		statusColor = ColorRed
	}
	fmt.Printf("%s=================== 客户端流式响应 =======================%s\n", ColorPurple, ColorReset)
	fmt.Printf("%s[客户端流式]%s %s%d%s %s%dms%s\n", ColorPurple, ColorReset, statusColor, status, ColorReset, ColorGray, duration.Milliseconds(), ColorReset)
	if body != nil {
		printJSON(body)
	}
	fmt.Printf("%s==========================================================%s\n", ColorPurple, ColorReset)
}

func Banner(port int, endpointMode string) {
	fmt.Printf(`
%s╔════════════════════════════════════════════════════════════╗
║           %sAntigravity2API%s - Go Version                      ║
╚════════════════════════════════════════════════════════════╝%s
`, ColorCyan, ColorGreen, ColorCyan, ColorReset)

	Info("Server starting on port %d", port)
	Info("Endpoint mode: %s", endpointMode)
	Info("Debug level: %s", config.Get().Debug)

	if os.Getenv("API_KEY") == "" {
		Warn("API_KEY not set - API authentication disabled")
	}

	fmt.Println()
}

func printJSON(v any) {
	if currentLogLevel == LogOff {
		return
	}
	sanitized := sanitizeJSONForLog(v)
	jsonBytes, err := json.MarshalIndent(sanitized, "", "  ")
	if err != nil {
		fmt.Printf("%v\n", v)
		return
	}
	fmt.Println(string(jsonBytes))
}

func formatRawJSON(rawJSON []byte) string {
	if currentLogLevel == LogOff {
		return ""
	}
	var data any
	if err := json.Unmarshal(rawJSON, &data); err != nil {
		return string(rawJSON)
	}

	sanitized := sanitizeJSONForLog(data)
	jsonBytes, err := json.MarshalIndent(sanitized, "", "  ")
	if err != nil {
		return string(rawJSON)
	}
	return string(jsonBytes)
}

func truncateBase64(s string) string {
	return truncateBase64Maybe(s, false)
}

func truncateBase64Maybe(s string, force bool) string {
	if len(s) <= 100 {
		return s
	}

	const keep = 20
	const markerFmt = "%s...[TRUNCATED: %d chars]...%s"

	// Handle data URLs or embedded base64 sections like:
	// - data:image/png;base64,iVBOR...
	// - ![image](data:image/jpeg;base64,/9j/...)  (markdown wrapper)
	if strings.Contains(s, ";base64,") {
		idx := strings.Index(s, ";base64,")
		if idx != -1 {
			prefix := s[:idx+len(";base64,")]
			rest := s[idx+len(";base64,"):]

			// If this looks like markdown, keep trailing ')' (and anything after) intact.
			base64Part := rest
			suffix := ""
			if end := strings.Index(rest, ")"); end != -1 {
				base64Part = rest[:end]
				suffix = rest[end:]
			}

			if len(base64Part) <= 100 || len(base64Part) <= keep*2 {
				return s
			}

			omitted := len(base64Part) - keep*2
			return fmt.Sprintf("%s%s...[TRUNCATED: %d chars]...%s%s", prefix, base64Part[:keep], omitted, base64Part[len(base64Part)-keep:], suffix)
		}
	}

	isBase64 := force
	if !isBase64 {
		// Universal base64 character check for very long strings.
		// Only sample the first 100 characters for performance.
		if len(s) > 200 {
			sampleLen := 100
			if len(s) < sampleLen {
				sampleLen = len(s)
			}
			looksLikeBase64 := true
			for i := 0; i < sampleLen; i++ {
				c := s[i]
				if !((c >= 'A' && c <= 'Z') ||
					(c >= 'a' && c <= 'z') ||
					(c >= '0' && c <= '9') ||
					c == '+' || c == '/' || c == '=') {
					looksLikeBase64 = false
					break
				}
			}
			if looksLikeBase64 {
				isBase64 = true
			}
		}
	}
	if !isBase64 {
		prefixes := []string{"/9j/", "iVBOR", "R0lGOD", "UklGR", "Qk1", "AAAA"}
		for _, p := range prefixes {
			if strings.HasPrefix(s, p) {
				isBase64 = true
				break
			}
		}
	}

	if !isBase64 || len(s) <= keep*2 {
		return s
	}

	omitted := len(s) - keep*2
	return fmt.Sprintf(markerFmt, s[:keep], omitted, s[len(s)-keep:])
}

func sanitizeJSONForLog(v any) any {
	return sanitizeJSONForLogContext(v, false)
}

func sanitizeJSONForLogContext(v any, inInlineData bool) any {
	switch val := v.(type) {
	case map[string]any:
		isSourceBase64Context := false
		if t, ok := val["type"].(string); ok && strings.TrimSpace(t) == "base64" {
			isSourceBase64Context = true
		}

		out := make(map[string]any, len(val))
		for k, child := range val {
			if k == "inlineData" {
				out[k] = sanitizeJSONForLogContext(child, true)
				continue
			}
			if k == "data" && (inInlineData || isSourceBase64Context) {
				if s, ok := child.(string); ok {
					out[k] = truncateBase64Maybe(s, true)
					continue
				}
			}
			if k == "url" {
				if s, ok := child.(string); ok {
					if strings.Contains(s, ";base64,") && len(s) > 100 {
						out[k] = truncateBase64Maybe(s, true)
						continue
					}
				}
			}
			if k == "content" {
				if s, ok := child.(string); ok {
					if strings.Contains(s, "![image](data:") && strings.Contains(s, ";base64,") && len(s) > 100 {
						out[k] = truncateBase64Maybe(s, true)
						continue
					}
				}
			}
			out[k] = sanitizeJSONForLogContext(child, inInlineData)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = sanitizeJSONForLogContext(item, inInlineData)
		}
		return out
	case string:
		if strings.Contains(val, ";base64,") && len(val) > 100 {
			return truncateBase64Maybe(val, true)
		}
		return truncateBase64Maybe(val, inInlineData)
	case nil, bool,
		float64, float32,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return v
	default:
		// Support structs/custom types logged via printJSON (e.g., OpenAI/Gemini/Claude response structs).
		b, err := json.Marshal(val)
		if err != nil {
			return v
		}
		var decoded any
		if err := json.Unmarshal(b, &decoded); err != nil {
			return v
		}
		return sanitizeJSONForLogContext(decoded, inInlineData)
	}
}
