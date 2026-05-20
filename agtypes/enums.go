// Copyright 2026 The antigravity-sdk-go Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package agtypes

// ThinkingLevel is the thinking level for Gemini models that support extended
// thinking. It controls the amount of reasoning the model performs before
// responding. See
// https://ai.google.dev/gemini-api/docs/thinking#thinking-levels for details.
type ThinkingLevel string

// Thinking levels.
const (
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
)

// BuiltinTools identifies a common connection-provided builtin tool.
type BuiltinTools string

// Builtin tool identifiers.
const (
	ToolListDir       BuiltinTools = "list_directory"   // List directory contents.
	ToolSearchDir     BuiltinTools = "search_directory" // Search within directories (grep).
	ToolFindFile      BuiltinTools = "find_file"        // Find files by name within a directory.
	ToolViewFile      BuiltinTools = "view_file"        // View file contents.
	ToolCreateFile    BuiltinTools = "create_file"      // Create a new file.
	ToolEditFile      BuiltinTools = "edit_file"        // Edit an existing file.
	ToolRunCommand    BuiltinTools = "run_command"      // Execute a shell command.
	ToolAskQuestion   BuiltinTools = "ask_question"     // Ask the user a clarifying question.
	ToolStartSubagent BuiltinTools = "start_subagent"   // Invoke a subagent.
	ToolGenerateImage BuiltinTools = "generate_image"   // Generate or edit images.
	ToolFinish        BuiltinTools = "finish"           // Finish the conversation and return structured output.
)

// ReadOnlyTools returns the builtin tools that only read state (no writes,
// deletes, or commands).
func ReadOnlyTools() []BuiltinTools {
	return []BuiltinTools{
		ToolListDir,
		ToolSearchDir,
		ToolFindFile,
		ToolViewFile,
		ToolFinish,
	}
}

// NondestructiveTools returns the builtin tools that cannot delete content.
func NondestructiveTools() []BuiltinTools {
	return []BuiltinTools{
		ToolListDir,
		ToolSearchDir,
		ToolFindFile,
		ToolViewFile,
		ToolCreateFile,
		ToolEditFile,
		ToolAskQuestion,
		ToolStartSubagent,
		ToolGenerateImage,
		ToolFinish,
	}
}

// AllTools returns all builtin tools.
func AllTools() []BuiltinTools {
	return []BuiltinTools{
		ToolListDir,
		ToolSearchDir,
		ToolFindFile,
		ToolViewFile,
		ToolCreateFile,
		ToolEditFile,
		ToolRunCommand,
		ToolAskQuestion,
		ToolStartSubagent,
		ToolGenerateImage,
		ToolFinish,
	}
}

// FileTools returns the builtin tools that perform file read/write/create
// operations. These tools accept a file path argument and can be scoped to
// specific workspace directories via the policy package's WorkspaceOnly helper.
func FileTools() []BuiltinTools {
	return []BuiltinTools{
		ToolViewFile,
		ToolCreateFile,
		ToolEditFile,
	}
}

// NoTools returns an empty tool list (no builtin tools).
func NoTools() []BuiltinTools {
	return []BuiltinTools{}
}

// StepType is the high-level type of a step.
type StepType string

// Step types.
const (
	StepTypeTextResponse  StepType = "TEXT_RESPONSE"
	StepTypeToolCall      StepType = "TOOL_CALL"
	StepTypeSystemMessage StepType = "SYSTEM_MESSAGE"
	StepTypeCompaction    StepType = "COMPACTION"
	StepTypeFinish        StepType = "FINISH"
	StepTypeUnknown       StepType = "UNKNOWN"
)

// StepSource is the source of a step.
type StepSource string

// Step sources.
const (
	StepSourceSystem  StepSource = "SYSTEM"
	StepSourceUser    StepSource = "USER"
	StepSourceModel   StepSource = "MODEL"
	StepSourceUnknown StepSource = "UNKNOWN"
)

// StepTarget is the target of a step interaction.
type StepTarget string

// Step targets.
const (
	StepTargetUser        StepTarget = "TARGET_USER"
	StepTargetEnvironment StepTarget = "TARGET_ENVIRONMENT"
	StepTargetUnspecified StepTarget = "TARGET_UNSPECIFIED"
	StepTargetUnknown     StepTarget = "UNKNOWN"
)

// StepStatus is the status of a step.
type StepStatus string

// Step statuses.
const (
	StepStatusActive         StepStatus = "ACTIVE"
	StepStatusDone           StepStatus = "DONE"
	StepStatusWaitingForUser StepStatus = "WAITING_FOR_USER"
	StepStatusError          StepStatus = "ERROR"
	StepStatusCanceled       StepStatus = "CANCELED"
	StepStatusUnknown        StepStatus = "UNKNOWN"
)

// TriggerDelivery controls how trigger messages are delivered to the agent.
type TriggerDelivery string

// Trigger delivery modes.
const (
	// TriggerSendImmediately sends immediately (non-blocking).
	TriggerSendImmediately TriggerDelivery = "send_immediately"
	// TriggerWaitIdle waits until the agent is idle before sending.
	TriggerWaitIdle TriggerDelivery = "wait_idle"
	// Note: an INTERRUPT mode (cancel current turn, then send) is deferred
	// upstream due to safety implications for in-flight tool calls.
)

// FileChangeKind is the kind of filesystem change detected by a file-watching
// trigger.
type FileChangeKind string

// File change kinds.
const (
	FileChangeAdded    FileChangeKind = "added"
	FileChangeModified FileChangeKind = "modified"
	FileChangeDeleted  FileChangeKind = "deleted"
)
