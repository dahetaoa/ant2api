package common

import "strings"

// ExtractTextFromContent 从 OpenAI/Claude 常见的 content/system 字段中提取纯文本：
// - string：直接返回
// - []any：抽取 {"type":"text","text":...} 并按 sep 连接
func ExtractTextFromContent(content any, sep string, skipEmpty bool) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		first := true
		for _, it := range v {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			if m["type"] != "text" {
				continue
			}
			t, _ := m["text"].(string)
			if skipEmpty && t == "" {
				continue
			}
			if !first {
				b.WriteString(sep)
			}
			b.WriteString(t)
			first = false
		}
		return b.String()
	default:
		return ""
	}
}

// ExtractSystemFromMessages 从一组消息中提取 role=="system" 的文本，并以两个换行分隔。
// 该函数用于 OpenAI 兼容请求的 system 指令拼接。
func ExtractSystemFromMessages[T any](messages []T, role func(T) string, content func(T) any) string {
	var b strings.Builder
	first := true
	for _, m := range messages {
		if role(m) != "system" {
			continue
		}
		t := ExtractTextFromContent(content(m), "\n", false)
		if t == "" {
			continue
		}
		if !first {
			b.WriteString("\n\n")
		}
		b.WriteString(t)
		first = false
	}
	return b.String()
}

// ExtractClaudeSystemText 提取 Claude 请求中的 system 字段文本（支持 string 与 []any）。
func ExtractClaudeSystemText(system any) string {
	return ExtractTextFromContent(system, "\n\n", true)
}
