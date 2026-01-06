package vertex

// SanitizeContents drops invalid/empty contents/parts before sending to Vertex.
//
// Vertex Content.Parts elements must set at least one of:
// - text (non-empty)
// - functionCall
// - functionResponse
// - inlineData
//
// Additionally, `thought=true` parts must also include a non-empty text field.
func SanitizeContents(contents []Content) []Content {
	if len(contents) == 0 {
		return contents
	}

	out := make([]Content, 0, len(contents))
	for _, c := range contents {
		if len(c.Parts) == 0 {
			continue
		}
		parts := make([]Part, 0, len(c.Parts))
		for _, p := range c.Parts {
			if p.FunctionCall != nil || p.FunctionResponse != nil || p.InlineData != nil {
				parts = append(parts, p)
				continue
			}

			if p.Text == "" {
				// Drop thought-only / signature-only / empty parts.
				continue
			}
			parts = append(parts, p)
		}
		if len(parts) == 0 {
			continue
		}
		c.Parts = parts
		out = append(out, c)
	}
	return out
}
