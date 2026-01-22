package json

import "github.com/bytedance/sonic"

var api = sonic.Config{
	EscapeHTML:  false,
	SortMapKeys: false,
	UseInt64:    true,
	CopyString:  true, // 防止解码后的字符串引用原始 JSON 缓冲区，避免内存泄漏
}.Froze()

func Marshal(v any) ([]byte, error) { return api.Marshal(v) }

func Unmarshal(data []byte, v any) error { return api.Unmarshal(data, v) }

func MarshalString(v any) (string, error) { return api.MarshalToString(v) }

func UnmarshalString(data string, v any) error { return api.UnmarshalFromString(data, v) }

func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return api.MarshalIndent(v, prefix, indent)
}
