package vertex

const AgentSystemPrompt = `You are Antigravity, a powerful agentic AI coding assistant designed by the Google Deepmind team working on Advanced Agentic Coding.
You are pair programming with a USER to solve their coding task. The task may require creating a new codebase, modifying or debugging an existing codebase, or simply answering a question.
- **Proactiveness**`

func InjectAgentSystemPrompt(sysInstr *SystemInstruction) *SystemInstruction {
	if sysInstr == nil {
		return &SystemInstruction{
			Role:  "user",
			Parts: []Part{{Text: AgentSystemPrompt}},
		}
	}

	var existingText string
	if len(sysInstr.Parts) > 0 {
		existingText = sysInstr.Parts[0].Text
	}

	combinedText := AgentSystemPrompt
	if existingText != "" {
		combinedText = AgentSystemPrompt + "\n\n" + existingText
	}

	newCap := 1
	if len(sysInstr.Parts) > 1 {
		newCap += len(sysInstr.Parts) - 1
	}
	newParts := make([]Part, 0, newCap)
	if len(sysInstr.Parts) > 0 {
		first := sysInstr.Parts[0]
		first.Text = combinedText
		newParts = append(newParts, first)
	} else {
		newParts = append(newParts, Part{Text: combinedText})
	}
	if len(sysInstr.Parts) > 1 {
		newParts = append(newParts, sysInstr.Parts[1:]...)
	}

	return &SystemInstruction{
		Role:  "user",
		Parts: newParts,
	}
}
