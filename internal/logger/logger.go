package logger

import (
	"anti2api-golang/refactor/internal/config"
	"bytes"
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
	jsonBytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Printf("%v\n", v)
		return
	}
	fmt.Println(string(jsonBytes))
}

func formatRawJSON(rawJSON []byte) string {
	var indented bytes.Buffer
	if err := json.Indent(&indented, rawJSON, "", "  "); err != nil {
		return string(rawJSON)
	}
	return indented.String()
}
