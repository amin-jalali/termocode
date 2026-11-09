package app

import "github.com/amin-jalali/termocode/internal/ai"

// aiParityTools are the chat's tools of the AI parity work that have no
// group of their own: the shared task list, saved chats, the IDE ops no
// typed tool does, specs and saved prompts.
func aiParityTools(root string, call aiIDECaller) []ai.Tool {
	return []ai.Tool{
		aiTrackerTasksTool(root), // ai_tracker_manage_tools.go
		aiChatManageTool(call),   // ai_chat_manage_tool.go
		aiIDEOpTool(call),        // ai_ide_op_tool.go
		aiSpecsTool(root),        // ai_specs_prompts_tools.go
		aiPromptActionsTool(),
	}
}
