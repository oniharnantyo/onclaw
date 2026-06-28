## 🧠 Tool Dispatcher — Dual-Format Parsing

```zig
// ─── Supported Formats ───
// 1. OpenAI Native JSON: 
//    {"tool_calls": [{"type": "function", "function": {"name": "shell", "arguments": "{...}"}}]}
// 2. XML tags: 
//    <tool_call name="shell">{"command":"ls"}</tool_call上升到[/tool_call]
// 3. Bracket tags: 
//    [tool_call name="read"]{"file":"README.md"}[/tool_call]
// 4. Compact (end-of-stream): 
//    <tool_call name="memory_list">{"limit":10}
```

### Parsing Logic

```zig
pub fn parseToolCalls(
    allocator: std.mem.Allocator,
    response: []const u8
) !ParseResult {
    // ─── Path 1: Try OpenAI native JSON format ───
    if (isNativeJsonFormat(response)) {
        const native = parseNativeToolCalls(allocator, response) catch null;
        if (native) |result| {
            if (result.calls.len > 0) return result;
        }
    }
    
    // ─── Path 2: Fall back to XML tag parsing ───
    return parseXmlToolCalls(allocator, response);
}
```

### XML Parsing Algorithm

```zig
fn parseXmlToolCalls(
    allocator: std.mem.Allocator,
    response: []const u8
) !ParseResult {
    var text_parts: std.ArrayListUnmanaged([]const u8) = .empty;
    var calls: std.ArrayListUnmanaged(ParsedToolCall) = .empty;
    var remaining = response;
    
    while (true) {
        // ─── Find next tool call marker ───
        const marker_info = findNextMarker(remaining) orelse break;
        // Searches for: <invoke name="...">, [TOOL_CALL], or [tool_call]
        
        // ─── Extract text before marker ───
        const before = std.mem.trim(u8, remaining[0..marker_info.start], " \t\r\n");
        if (before.len > 0) {
            try text_parts.append(allocator, before);
        }
        
        // ─── Find closing tag ───
        const found_end = findClosingTag(after_open) orelse {
            // Unclosed tag - attempt recovery for compact format
            try recoverUnclosedTag(allocator, after_open, &calls);
            break;
        };
        
        // ─── Parse inner JSON ───
        const inner = std.mem.trim(u8, after_open[0..found_end], " \t\r\n");
        const parsed_call = try parseInnerToolCall(allocator, inner);
        
        if (parsed_call != null) {
            try calls.append(allocator, parsed_call.?);
        }
        
        remaining = after_open[found_end + end_tag_len..];
    }
    
    // ─── Join text parts ───
    const text = try std.mem.join(allocator, "\n", text_parts.items);
    
    // ─── Strip tool result markup ───
    const sanitized_text = try stripToolResultMarkup(allocator, text);
    
    return .{
        .text = sanitized_text,
        .calls = try calls.toOwnedSlice(allocator),
    };
}
```

## 📚 History Compaction — Token Management

### Auto-Compaction Triggers

```zig
pub fn autoCompactHistory(
    allocator: std.mem.Allocator,
    history: *std.ArrayListUnmanaged(OwnedMessage),
    provider: Provider,
    model_name: []const u8,
    config: CompactionConfig,
    redactor: ?*redaction.Redactor
) !bool {
    // ─── Trigger 1: Message count exceeds threshold ───
    const count_trigger = non_system_count > config.max_history_messages;
    
    // ─── Trigger 2: Token estimate exceeds 75% of limit ───
    const token_threshold = (config.token_limit * 3) / 4;
    const token_trigger = tokenEstimate(history.items) > token_threshold;
    
    if (!count_trigger and !token_trigger) {
        return false;
    }
    
    // ─── Perform compaction ───
    const summary = try summarizeHistory(allocator, history, provider, model_name, config);
    defer allocator.free(summary);
    
    // ─── Keep recent messages ───
    const keep_count = @min(config.keep_recent, non_system_count);
    const start_idx = history.items.len - keep_count;
    
    // ─── Rebuild history ───
    const new_history = try allocator.alloc(OwnedMessage, keep_count + 1);
    new_history[0] = .{ .role = .system, .content = history.items[0].content };
    
    for (0..keep_count) |i| {
        new_history[i + 1] = history.items[start_idx + i];
    }
    
    // ─── Insert summary ───
    const summary_msg = .{
        .role = .system,
        .content = try std.fmt.allocPrint(
            allocator,
            "[Conversation Summary]\n\n{s}",
            .{summary}
        ),
    };
    
    history.clearRetainingCapacity();
    try history.append(allocator, summary_msg);
    
    return true;
}
```

## 🧩 Memory Context Injection

```zig
pub fn enrichMessageWithRuntime(
    allocator: std.mem.Allocator,
    mem: Memory,
    mem_rt: ?*MemoryRuntime,
    user_message: []const u8,
    session_id: ?[]const u8
) ![]const u8 {
    // ─── Search memories (hybrid search, RRF, temporal decay, MMR) ───
    const scoped_entries = mem.recall(
        allocator,
        user_message,
        SCOPED_RECALL_CANDIDATE_LIMIT,  // 64 candidates
        session_id
    ) catch {
        return try allocator.dupe(u8, user_message);
    };
    defer memory_mod.freeEntries(allocator, scoped_entries);
    
    if (scoped_entries.len == 0) {
        return try allocator.dupe(u8, user_message);
    }
    
    // ─── Build memory context preamble ───
    var buf: std.ArrayListUnmanaged(u8) = .empty;
    var buf_writer: std.Io.Writer.Allocating = .fromArrayList(allocator, &buf);
    const w = &buf_writer.writer;
    
    try w.print("[Memory context]\n", .{});
    
    var total_bytes: usize = 0;
    const max_bytes = MAX_CONTEXT_BYTES;  // 4KB limit
    
    for (scoped_entries) |entry| {
        if (total_bytes >= max_bytes) break;
        
        const sanitized = try sanitizeMemoryText(allocator, entry.content);
        defer allocator.free(sanitized);
        
        const entry_text = try std.fmt.allocPrint(
            allocator,
            "- {s}: {s}\n",
            .{ entry.key, sanitized }
        );
        defer allocator.free(entry_text);
        
        if (total_bytes + entry_text.len > max_bytes) {
            break;
        }
        
        try w.writeAll(entry_text);
        total_bytes += entry_text.len;
    }
    
    try w.print("\n[user]\n{s}\n", .{user_message});
    
    return try buf.toOwnedSlice(allocator);
}
```

## ⚡ Slash Command System (40+ Commands)

```zig
const SlashCommandKind = enum {
    new_reset,       // /new, /reset
    restart,         // /restart
    help,            // /help, /commands
    status,          // /status
    whoami,          // /whoami, /id
    model,           // /model, /models
    think,           // /think, /thinking, /t
    verbose,         // /verbose, /v
    reasoning,       // /reasoning, /reason
    exec,            // /exec
    queue,           // /queue
    usage,           // /usage
    tts,             // /tts, /voice
    stop,            // /stop, /abort
    compact,         // /compact
    allowlist,       // /allowlist
    approve,         // /approve
    context,         // /context
    export_session,  // /export-session
    session,         // /session
    subagents,       // /subagents, /tasks
    agents,          // /agents
    focus,           // /focus
    unfocus,         // /unfocus
    kill,            // /kill
    steer,           // /steer
    tell,            // /tell
    config,          // /config
    capabilities,    // /capabilities
    debug,           // /debug
    dock_telegram,   // /dock-telegram
    dock_discord,    // /dock-discord
    dock_slack,      // /dock-slack
    activation,      // /activation
    send,            // /send
    elevated,        // /elevated
    bash,            // /bash
    poll,            // /poll
    skill,           // /skill, /skills
    doctor,          // /doctor
    memory,          // /memory, /mem
    cost,            // /cost, /costs
    unknown,
};

pub fn planTurnInput(message: []const u8) TurnInputPlan {
    const cmd = parseSlashCommand(message) orelse 
        return .{ .llm_user_message = message };
    
    const kind = classifySlashCommand(cmd);
    
    if (kind != .unknown) {
        return .{
            .clear_session = slashCommandClearsSession(kind),
            .invoke_local_handler = true,
            .llm_user_message = null,
        };
    }
    
    return .{ .llm_user_message = message };
}
```

## 🎯 Key Design Principles

### 1. Arena Allocators for Turn-Scoped Memory

```zig
var iter_arena = std.heap.ArenaAllocator.init(self.allocator);
defer iter_arena.deinit();

while (iteration < max_iterations) : (iteration += 1) {
    // Reset retains capacity → no reallocations
    _ = iter_arena.reset(.retain_capacity);
    const arena = iter_arena.allocator();
    
    // All turn allocations use arena
    const messages = try self.buildProviderMessagesForTurn(arena, ...);
    const parsed = try dispatcher.parseToolCalls(arena, response_text);
    // Freed automatically at end of iteration
}
```

### 2. Owned Messages for History

```zig
pub const OwnedMessage = struct {
    role: providers.Role,
    content: []const u8,  // Heap-allocated, owned by history
    
    pub fn deinit(self: *const OwnedMessage, allocator: std.mem.Allocator) void {
        allocator.free(self.content);
    }
};

fn dupeForHistory(self: *Agent, content: []const u8) ![]const u8 {
    if (self.redactor) |r| 
        return r.redact(self.allocator, content);
    return self.allocator.dupe(u8, content);
}
```

### 3. Fingerprint-Based Caching

```zig
// System prompt fingerprinting
const workspace_fp = prompt.workspacePromptFingerprint(
    self.allocator, self.workspace_dir, self.bootstrap, cfg.identity
);

// Only rebuild if workspace or model changed
if (self.workspace_prompt_fingerprint != workspace_fp or
    self.system_prompt_model_name != turn_model_name)
{
    // Rebuild system prompt
}

// Response cache key
const key_hex = cache.ResponseCache.cacheKeyHex(
    &key_buf, turn_model_name, system_prompt, safe_user_message
);
```

### 4. Interrupt Handling

```zig
// Atomic flag for cross-thread interrupts
interrupt_requested: std.atomic.Value(bool),

pub fn requestInterrupt(self: *Agent) void {
    self.interrupt_requested.store(true, .seq_cst);
}

fn isInterruptRequested(self: *Agent) bool {
    return self.interrupt_requested.load(.seq_cst);
}

// In tool execution
if (self.isInterruptRequested()) {
    return .{
        .name = call.name,
        .output = "Interrupted by /stop",
        .success = false,
    };
}

// Thread-local flag for process utilities
tools_mod.process_util.setThreadInterruptFlag(&self.interrupt_requested);
```

### 5. Redaction Integration

```zig
// Per-conversation redactor
redactor: ?*redaction.Redactor,

// Redact user message before LLM call
const safe_user_message = if (self.redactor) |r|
    r.redact(self.allocator, effective_user_message)
else
    effective_user_message;

// Redact for history
fn dupeForHistory(self: *Agent, content: []const u8) ![]const u8 {
    if (self.redactor) |r| 
        return r.redact(self.allocator, content);
    return self.allocator.dupe(u8, content);
}

// Don't rehydrate placeholders in tool args
// (prevents provider → tool PII leak)
fn executeTool(...) ToolExecutionResult {
    // Parse args with placeholders intact
    var parsed = std.json.parseFromSlice(
        std.json.Value, tool_allocator, call.arguments_json, .{}
    ) catch {
        return .{ .output = "Invalid JSON", .success = false };
    };
    // Placeholders passed through unchanged
}
```

## 📊 Performance Characteristics

### Memory Management

```zig
// Turn-scoped allocations: freed in batches
var iter_arena = std.heap.ArenaAllocator.init(self.allocator);
defer iter_arena.deinit();

// History allocations: owned manually
history: std.ArrayListUnmanaged(OwnedMessage),

// Response cache: bounded LRU
response_cache: ?*cache.ResponseCache,

// Compaction: automatic when token threshold exceeded
self.maybeAutoCompactHistory();
```

### Token Management

```zig
// Token estimation: (chars + 3) / 4
pub fn tokenEstimate(history: []const OwnedMessage) u64 {
    var total_chars: u64 = 0;
    for (history) |*msg| {
        total_chars += msg.content.len;
    }
    return (total_chars + 3) / 4;
}

// Context limit: 75% threshold triggers compaction
const token_threshold = (config.token_limit * 3) / 4;
if (tokenEstimate(history.items) > token_threshold) {
    try autoCompactHistory(...);
}
```

### Cost Tracking

```zig
// Track tokens and cost per turn
self.total_tokens += normalized_usage.total_tokens;
self.total_cost_usd += cost_mod.TokenUsage.fromProviders(
    turn_model_name, normalized_usage
).cost();

// Emit usage records
if (self.usage_record_callback) |cb| {
    cb(self.usage_record_ctx, .{
        .ts = std_compat.time.nanoTimestamp(),
        .provider = provider_name,
        .model = turn_model_name,
        .usage = normalized_usage,
        .success = true,
    });
}
```

## 🔒 Security & Autonomy

### Policy Enforcement

```zig
policy: ?*const SecurityPolicy,

// In executeTool
if (self.policy) |pol| {
    // Read-only check
    if (!pol.canAct()) {
        return .{ .output = "Blocked: read-only mode", .success = false };
    }
    
    // Rate limiting
    if (!pol.recordAction()) {
        return .{ .output = "Rate limit exceeded", .success = false };
    }
}
```

### Exec Tool Security

```zig
fn isExecToolName(name: []const u8) bool {
    return std.ascii.eqlIgnoreCase(name, "exec") or
           std.ascii.eqlIgnoreCase(name, "bash") or
           std.ascii.eqlIgnoreCase(name, "shell");
}

fn execBlockMessage(args: std.json.ObjectMap) ?[]const u8 {
    // Check exec_security level
    if (self.exec_security == .deny) {
        return "exec is blocked: security level=deny";
    }
    
    // Check allowlist
    if (self.exec_security == .allowlist) {
        if (!self.isCommandAllowed(command)) {
            return "exec is blocked: command not in allowlist";
        }
    }
    
    // Check exec_ask mode
    if (self.exec_ask == .always or 
        (self.exec_ask == .on_miss && !self.isCommandAllowed(command)))
    {
        return "exec is blocked: awaiting approval";
    }
    
    return null;
}
```

## 🎛️ Configuration System

### Agent Profiles

```zig
pub const NamedAgentConfig = struct {
    name: []const u8,
    provider: []const u8,
    model: []const u8,
    temperature: ?f64 = null,
    system_prompt: ?[]const u8 = null,
    workspace_path: ?[]const u8 = null,
    enable_pii_redaction: bool = true,
    max_tool_iterations: ?u32 = null,
    max_history_messages: ?u32 = null,
};

// Load agent with profile
var agent = try Agent.fromConfigWithProfile(
    allocator,
    &cfg,
    provider,
    tools,
    mem,
    observer,
    profile  // Optional: use default agent config if null
);
```

### Model Routing

```zig
// Intelligent routing based on user intent
const route_selection = self.routeSelectionForTurn(user_message);

// Route degradation on errors
if (self.routeShouldBeDegraded(err)) {
    try self.markRouteDegraded(selection, err);
}

// Fallback providers
fallback_providers: []const []const u8,

// Model fallbacks within provider
model_fallbacks: []const config_types.ModelFallbackEntry,
```

`★ Insight ─────────────────────────────────────`
**NullClaw's agent architecture proves that constraints drive innovation** — the 678KB binary target forces careful design: arena allocators for turn-scoped memory, vtable polymorphism for extensibility, fingerprint-based caching for efficiency, and atomic interrupt flags for graceful cancellation. Every byte serves a purpose.
`─────────────────────────────────────────────────`

This is a production-grade autonomous agent runtime that demonstrates how Zig's zero-cost abstractions enable sophisticated AI systems without the bloat of traditional frameworks. The combination of careful resource management, security-first design, and extensive observability makes it suitable for both local development and production deployments.