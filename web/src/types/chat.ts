/* ============================================================
   Chat Type Definitions — matching eino AgenticMessage wire format
   Source of truth: internal/agent/agentic_message.go
   ============================================================ */

// ── Content Block Types (from eino's schema.ContentBlockType) ──

export type ContentBlockType =
  | 'user_input_text'
  | 'assistant_gen_text'
  | 'reasoning'
  | 'function_tool_call'
  | 'function_tool_result'
  | 'mcp_tool_call'
  | 'mcp_tool_result'
  | 'user_input_image'
  | 'user_input_file'
  | 'tool_call'
  | 'tool_result';

// ── Text Blocks ────────────────────────────────────────────────

export interface UserInputText {
  text: string;
}

export interface AssistantGenText {
  text: string;
  open_ai_extension?: Record<string, unknown>;
  claude_extension?: Record<string, unknown>;
  extension?: Record<string, unknown>;
}

// ── Reasoning Block ────────────────────────────────────────────

export interface Reasoning {
  text: string;
  signature?: string;
  open_ai_extension?: Record<string, unknown>;
}

// ── Function Tool Blocks ───────────────────────────────────────

export interface FunctionToolCall {
  id: string;
  name: string;
  arguments: string; // JSON string
}

export interface FunctionToolResultContentBlock {
  type: string;
  text?: UserInputText;
  image?: Record<string, unknown>;
  audio?: Record<string, unknown>;
  video?: Record<string, unknown>;
  file?: Record<string, unknown>;
  extra?: Record<string, unknown>;
}

export interface FunctionToolResult {
  call_id: string;
  name: string;
  content: FunctionToolResultContentBlock[];
}

// ── MCP Tool Blocks ────────────────────────────────────────────

export interface MCPToolCall {
  [key: string]: unknown;
}

export interface MCPToolResult {
  [key: string]: unknown;
}

export interface MCPListToolsResult {
  [key: string]: unknown;
}

export interface MCPToolApprovalRequest {
  [key: string]: unknown;
}

export interface MCPToolApprovalResponse {
  [key: string]: unknown;
}

// ── Server Tool Blocks ─────────────────────────────────────────

export interface ServerToolCall {
  [key: string]: unknown;
}

export interface ServerToolResult {
  [key: string]: unknown;
}

// ── Media Blocks ───────────────────────────────────────────────

export interface UserInputImage {
  [key: string]: unknown;
}

export interface UserInputAudio {
  [key: string]: unknown;
}

export interface UserInputVideo {
  [key: string]: unknown;
}

export interface UserInputFile {
  [key: string]: unknown;
}

export interface AssistantGenImage {
  [key: string]: unknown;
}

export interface AssistantGenAudio {
  [key: string]: unknown;
}

export interface AssistantGenVideo {
  [key: string]: unknown;
}

// ── Streaming Metadata ─────────────────────────────────────────

// Stable per-block id stamped by the streaming transport so the client can
// merge delta fragments into the correct content block.
export interface StreamingMeta {
  index: number;
}

// ── Content Block (unified type) ───────────────────────────────

export interface ContentBlock {
  type: ContentBlockType;

  // text blocks
  user_input_text?: UserInputText;
  assistant_gen_text?: AssistantGenText;

  // user input media
  user_input_image?: UserInputImage;
  user_input_audio?: UserInputAudio;
  user_input_video?: UserInputVideo;
  user_input_file?: UserInputFile;

  // assistant generated media
  assistant_gen_image?: AssistantGenImage;
  assistant_gen_audio?: AssistantGenAudio;
  assistant_gen_video?: AssistantGenVideo;

  // reasoning
  reasoning?: Reasoning;

  // function tool
  function_tool_call?: FunctionToolCall;
  function_tool_result?: FunctionToolResult;

  // MCP tool
  mcp_tool_call?: MCPToolCall;
  mcp_tool_result?: MCPToolResult;
  mcp_list_tools_result?: MCPListToolsResult;
  mcp_tool_approval_request?: MCPToolApprovalRequest;
  mcp_tool_approval_response?: MCPToolApprovalResponse;

  // server tool
  server_tool_call?: ServerToolCall;
  server_tool_result?: ServerToolResult;

  // streaming metadata
  streaming_meta?: StreamingMeta;

  extra?: Record<string, unknown>;
}

// ── Message ────────────────────────────────────────────────────

export interface ChatMessage {
  id?: number;
  seq?: number;
  role: 'user' | 'assistant' | 'system';
  content_blocks?: ContentBlock[];
  created_at?: string;
  isStreaming?: boolean;
  stopped?: boolean;
  response_meta?: Record<string, unknown>;
  extra?: Record<string, unknown>;
}

// ── Conversation ────────────────────────────────────────────────

export interface Conversation {
  id: number;
  agent_name: string;
  message_count: number;
  created_at: string;
  updated_at: string;
  preview?: string;
}

// ── SSE Event Types ───────────────────────────────────────────

export interface SSEInitEvent {
  conversation_id: number;
  context_window?: number;
  agent_name?: string;
}

export interface SSETurnEvent {
  conversation_id: number;
  sequence_num: number;
  response_id: string;
  previous_response_id?: string;
  model: string;
  tokens: number;
  prompt_tokens?: number;
  completion_tokens?: number;
  total_tokens?: number;
}

export interface SSEMessageEvent {
  role: string;
  content_blocks: ContentBlock[];
  response_meta?: Record<string, unknown>;
  extra?: Record<string, unknown>;
}

export interface SSEErrorEvent {
  error: string;
}

export interface SSEDoneEvent {
  status: string;
}
