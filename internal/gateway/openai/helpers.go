package openai

import jsonpkg "anti2api-golang/refactor/internal/pkg/json"

func jsonString(v any) (string, error) { return jsonpkg.MarshalString(v) }

