# Parity Audit: antigravity-sdk-python → antigravity-sdk-go

This document maps every public symbol in upstream
[google-antigravity/antigravity-sdk-python](https://github.com/google-antigravity/antigravity-sdk-python)
to its Go counterpart, and enumerates the deliberate gaps and renames the port
introduced.

**Ported against upstream:**
- Repo commit `287894d3b5689b99fcea97900d05cfa7fe93fcbf` (tracks `main`).
- Wire bindings derived from `localharness_pb2.py` blob `b51c2f3` (the
  embedded `FileDescriptorProto`; the `.proto` source is not in the repo).

## Conventions

- **Module → package.** Python modules generally map onto a single Go package;
  some Python modules are collapsed into a sibling package when the boundary
  was an implementation detail.
- **Names.** Go uses initialisms (`HTTP`, `ID`, `MCP`) per Go conventions
  rather than Pydantic's `Http`, `Id`, `Mcp`. Snake_case names become
  PascalCase. The Pydantic `ToolWithSchema.__name__` / `__doc__` become
  explicit `Name` / `Description` struct fields.
- **Sum types.** Python `T | U` unions become a sealed interface with an
  unexported marker method for closed sets, or an `any` alias documented to
  accept the listed types when the union has no shared method set.
- **Validators.** Pydantic `model_validator(mode="after")` becomes either a
  `Validate() error` method on the receiver (in place) or a constructor that
  returns `(T, error)`.
- **Async iterators** become Go 1.23+ `iter.Seq2[V, error]`.
- **`async with` lifecycles** become explicit `Start(ctx)` / `Close(ctx)` pairs.
- **Hooks** keep the upstream taxonomy but are typed function values, not
  classes (see `hook/` row below).

## Module-by-module mapping

### `google.antigravity.__init__` (`__all__`)

| Python                 | Go                                         | Notes |
|------------------------|--------------------------------------------|-------|
| `Agent`                | `antigravity.Agent`                        | |
| `AgentConfig`          | `antigravity.AgentConfig` (alias `connection.AgentConfig`) | |
| `LocalAgentConfig`     | `antigravity.LocalAgentConfig` (alias `connection/local.AgentConfig`) | |
| `ToolContext`          | `antigravity.ToolContext` (alias `tool.ToolContext`) | |
| `CapabilitiesConfig`   | `antigravity.CapabilitiesConfig` (alias `agtypes.CapabilitiesConfig`) | |
| `GeminiConfig`         | `antigravity.GeminiConfig`                 | |
| `GenerationConfig`     | `antigravity.GenerationConfig`             | |
| `ModelConfig`          | `antigravity.ModelConfig`                  | |
| `ModelEntry`           | `antigravity.ModelEntry`                   | |
| `ThinkingLevel`        | `antigravity.ThinkingLevel`                | |
| `UsageMetadata`        | `antigravity.UsageMetadata`                | |

### `google.antigravity.agent` → root `package antigravity`

| Python                                         | Go                                  | Notes |
|------------------------------------------------|-------------------------------------|-------|
| `Agent(config)` async ctx mgr (`__aenter__`/`__aexit__`) | `New(ctx, config) (*Agent, error)` / `(*Agent).Close(ctx)` | Explicit lifecycle. `NewAgent(config)` + `Start(ctx)` is also available. |
| `agent.chat(prompt)`                           | `(*Agent).Chat(ctx, prompt)`        | |
| `agent.register_hook(h)`                       | `(*Agent).RegisterHook(h)`          | |
| `agent.register_trigger(t)`                    | `(*Agent).RegisterTrigger(t)`       | |
| `agent.is_started`                             | `(*Agent).IsStarted()`              | |
| `agent.conversation`                           | `(*Agent).Conversation()`           | panics if unstarted; pair with `IsStarted` |
| `agent.conversation_id`                        | `(*Agent).ConversationID()`         | |
| internal write-tools/MCP guard `ValueError`    | `ErrNoPolicy`                       | Same logic. Escape hatches: any policy (e.g. `policy.AllowAll()`) or a custom `PreToolCallDecide` hook. |
| internal `_upgrade_to_interactive_confirmation` | **dropped** — replaced by public `interactive.WithUserConfirmation(cfg, p)` | See gap §1. |

### `google.antigravity.types` → `agtypes/`

Python `__all__`:

| Python                                  | Go (`agtypes.*`)                   | Notes |
|-----------------------------------------|------------------------------------|-------|
| `ThinkingLevel`                         | `ThinkingLevel` (typed string + consts `ThinkingMinimal/Low/Medium/High`) | |
| `BuiltinTools` + classmethods           | `BuiltinTools` (typed string) + `ReadOnlyTools/NondestructiveTools/AllTools/FileTools/NoTools()` | |
| `StepType` / `StepSource` / `StepTarget` / `StepStatus` | same names, typed strings | |
| `TriggerDelivery` / `FileChangeKind`    | same                                | |
| `GenerationConfig`                      | `GenerationConfig`                  | |
| `ModelEntry`                            | `ModelEntry` + `NewModelEntry(name)` | string-coercion validator dropped (gap §3) |
| `ModelConfig` + defaults                | `ModelConfig` + `DefaultModelConfig()` | |
| `GeminiConfig`                          | `GeminiConfig`                      | |
| `SystemInstructionSection`              | `SystemInstructionSection` + `NewSystemInstructionSection(content)` | |
| `CustomSystemInstructions`              | `CustomSystemInstructions`          | |
| `TemplatedSystemInstructions`           | `TemplatedSystemInstructions`       | |
| `SystemInstructions` union              | `SystemInstructions` interface (sealed) | |
| `CapabilitiesConfig` + validator        | `CapabilitiesConfig` + `Validate()`, `ActiveBuiltinTools()`, `DefaultCapabilitiesConfig()` | |
| `McpStdioServer` / `McpSseServer` / `McpStreamableHttpServer` | `McpStdioServer` / `McpSseServer` / `McpStreamableHTTPServer` (+ `NewMcpStreamableHTTPServer`) | HTTP capitalization per Go convention (rename §1) |
| `McpServerConfig` union                 | `McpServerConfig` interface (sealed, + `MCPType()` discriminator) | |
| `ToolCall`                              | `ToolCall`                          | |
| `ToolResult`                            | `ToolResult`                        | |
| `PythonTool` type alias                 | **deferred** — see gap §2           | The Go callable is `tool.Tool`/`tool.ToolWithSchema`. |
| `UsageMetadata`                         | `UsageMetadata`                     | |
| `Step` (+ `extra="allow"`)              | `Step` (with `Extra map[string]any` via `json:",inline"`) | |
| `HookResult` (+ `default allow=True`)   | `HookResult` + `AllowHookResult()`  | Go zero value denies; use the constructor. |
| `QuestionResponse` / `QuestionHookResult` | same                              | |
| `AskQuestionOption` / `AskQuestionEntry` / `AskQuestionInteractionSpec` | same | |
| `AntigravityConnectionError`            | `ConnectionError` (with `Unwrap`)   | |
| `AntigravityValidationError`            | `ValidationError` (with `Unwrap`)   | `from_pydantic` dropped — gap §4 |
| `StreamChunk` union                     | `StreamChunk` interface (sealed)    | |
| `Thought` / `Text`                      | same                                | |
| `FileChange`                            | `FileChange`                        | |
| media `Image` / `Document` / `Audio` / `Video` | same, over `Media` interface; constructors `NewImage`/`NewDocument`/`NewAudio`/`NewVideo` return `(T, error)`; `FromFile(path, desc)` dispatcher | |
| `ContentPrimitive` / `Content` aliases  | `ContentPrimitive = any` / `Content = any` | documented union (gap §5) |
| `ChatResponse`                          | `antigravity.ChatResponse` (root)   | Moved to root because it back-references `*Conversation`; agtypes is the dependency root. |
|                                         | `ChatChunk = any` (new)             | Helper alias for the `StreamChunk | ToolCall` union surfaced by `Conversation.ReceiveChunks` / `ChatResponse.Chunks`. |

### `google.antigravity.conversation.conversation` → root `package antigravity`

| Python                                | Go                                          | Notes |
|---------------------------------------|---------------------------------------------|-------|
| `Conversation.create(strategy)` ctx mgr | `CreateConversation(ctx, strategy)` + `(*Conversation).Disconnect(ctx)` | Explicit lifecycle. |
| `Conversation(connection, max_history)` | `NewConversation(conn, maxHistorySize)`   | |
| `conversation.send(prompt)`           | `Send(ctx, prompt)`                         | |
| `conversation.receive_steps()`        | `ReceiveSteps(ctx) iter.Seq2[Step, error]` | Single-active-iterator invariant (`ErrIterating`). |
| `conversation.receive_chunks()`       | `ReceiveChunks(ctx) iter.Seq2[ChatChunk, error]` | per-turn tool-call dedup by ID |
| `conversation.chat(prompt)`           | `Chat(ctx, prompt) (*ChatResponse, error)` | returns immediately; consume the response to drive the stream |
| `conversation.get_last_structured_output()` | `LastStructuredOutput()`               | |
| `conversation.history`                | `History()` (returns a copy)               | |
| `conversation.last_response`          | `LastResponse()`                            | |
| `conversation.turn_count`             | `TurnCount()`                               | |
| `conversation.compaction_indices`     | `CompactionIndices()` (returns a copy)     | |
| `conversation.connection`             | `Connection()`                              | |
| `conversation.is_idle`                | `IsIdle()`                                  | |
| `conversation.conversation_id`        | `ConversationID()`                          | |
| `conversation.total_usage`            | `TotalUsage()`                              | |
| `conversation.last_turn_usage` (`None` when absent) | `LastTurnUsage() (UsageMetadata, bool)` | Go idiom for "optional" |
| `conversation.clear_history()`        | `ClearHistory()`                            | |
| `conversation.cancel/delete/signal_idle/wait_for_idle/wait_for_wakeup/disconnect()` | same names, `(ctx, ...)` | |
| `ChatResponse` (defined in `types.py`) | `antigravity.ChatResponse` with `Chunks()`/`Thoughts()`/`TextDeltas()`/`ToolCalls()`/`Text()`/`Resolve()`/`StructuredOutput()`/`UsageMetadata()`/`Close()` | multi-cursor shared-buffer streaming via `iter.Pull2` (option 2 contract: concurrent cursors, serialized pulls) |

### `google.antigravity.hooks.hooks` → `hook/`

| Python                                | Go                                          | Notes |
|---------------------------------------|---------------------------------------------|-------|
| `HookContext` / `SessionContext` / `TurnContext` / `OperationContext` | `hook.Context` + `NewSessionContext` / `NewTurnContext` / `NewOperationContext` | rename §2: collapsed to one type with constructors |
| `InspectHook[T]` / `DecideHook[T]` / `TransformHook[T,R]` (generic union) | **dropped** — replaced by 9 typed function values + sealed `Hook` marker interface | gap §6 |
| `pre_turn` / `post_turn` / `pre_tool_call_decide` / `post_tool_call` / `on_tool_error` / `on_interaction` / `on_compaction` / `on_session_start` / `on_session_end` decorators | `hook.PreTurn` / `PostTurn` / `PreToolCallDecide` / `PostToolCall` / `OnToolError` / `OnInteraction` / `OnCompaction` / `OnSessionStart` / `OnSessionEnd` function types | A user writes the typed function value directly; no decorator wrapper. |

### `google.antigravity.hooks.hook_runner` → `hook/`

| Python                                | Go (`hook.Runner`)                          | Notes |
|---------------------------------------|---------------------------------------------|-------|
| `HookRunner()`                        | `NewRunner()`                               | |
| `runner.register_hook(h)`             | `Register(h Hook)` + `ErrUnknownHook`       | type-switches over the 9 concrete hook types |
| `runner.has_hooks` etc.               | `HasHooks()`, `HasPreToolCallDecide()`      | per-category accessors as needed |
| `dispatch_*` async methods            | `DispatchSessionStart/End/PreTurn/PostTurn/PreToolCall/PostToolCall/OnToolError/Interaction/Compaction(ctx, ...)` | preserves upstream short-circuit semantics |
| `runner.session_context`              | `SessionContext()`                          | |

### `google.antigravity.hooks.policy` → `hook/policy/`

| Python                                | Go (`policy.*`)                             | Notes |
|---------------------------------------|---------------------------------------------|-------|
| `Decision` enum                       | `Decision` (int) + `Approve` / `Deny` / `AskUser` consts | |
| `Policy` model                        | `Policy` struct                             | |
| `Predicate` (3-mode dispatch)         | `Predicate = func(ctx, ToolCall) (bool, error)` | gap §7: always passed the ToolCall; the model-coercion / args-dict variants dropped |
| `AskUserHandler`                      | `AskUserHandler = func(ctx, ToolCall) (bool, error)` | |
| `allow(tool, ...)`                    | `Allow(tool, when, name) Policy`            | |
| `deny(tool, ...)`                     | `DenyTool(tool, when, name) Policy`         | rename §3 to avoid clash with `Deny` const |
| `ask_user(tool, handler, ...)`        | `Ask(tool, handler, when, name) Policy`     | |
| `allow_all()` / `deny_all()`          | `AllowAll()` / `DenyAll()`                  | |
| `safe_defaults(handler)`              | `SafeDefaults(handler) []Policy`            | |
| `confirm_run_command(handler)`        | `ConfirmRunCommand(handler) []Policy`       | |
| `is_path_in_workspace(t, ws)`         | `IsPathInWorkspace(target, ws) bool`        | **Hardened**: rejects raw `..` segments and resolves longest existing ancestor via `EvalSymlinks` to defeat `filepath.Clean`-before-symlink bypasses. See `policy/path.go`. |
| `workspace_only(workspaces)`          | `WorkspaceOnly(workspaces) []Policy`        | **Empty `CanonicalPath` denies** for file tools (fail closed, not open). |
| `enforce(policies)`                   | `Enforce(policies) (hook.PreToolCallDecide, error)` | Go funcs cannot raise — Enforce surfaces `ErrMissingAskUserHandler` at construction. |

### `google.antigravity.tools.tool_context` → `tool/`

| Python                                | Go (`tool.*`)                               | Notes |
|---------------------------------------|---------------------------------------------|-------|
| `ToolContext(connection)`             | `NewToolContext(conn) *ToolContext`         | `conn` is a narrow local interface (`ConversationID/IsIdle/SendTriggerNotification`) — connection-package independence |
| `tool_context.conversation_id` etc.   | `(*ToolContext).ConversationID/IsIdle/Send(ctx, msg)` | |
| `tool_context.get_state/set_state`    | `GetState(key)` / `SetState(key, value)` (concurrent-safe) | |
| signature-introspection injection     | `WithToolContext(ctx, tc)` / `FromContext(ctx) (*ToolContext, bool)` | gap §8: ToolContext is threaded via `context.Context`, not a hidden function parameter |

### `google.antigravity.tools.tool_runner` → `tool/`

| Python                                | Go (`tool.*`)                               | Notes |
|---------------------------------------|---------------------------------------------|-------|
| `ToolRunner()`                        | `NewRunner() *Runner`                       | |
| `runner.register/unregister/names/tools` | `Register/Unregister/Names/Schema/Description` | |
| `runner.execute(name, **kwargs)`      | `Execute(ctx, name, args map[string]any)`   | gap §8 |
| `runner.process_tool_calls(calls)`    | `ProcessToolCalls(ctx, calls)` (concurrent via `wg.Go`) | |
| `runner.set_context(tc)`              | `SetContext(*ToolContext)`                  | |
| `ToolWithSchema`                      | `ToolWithSchema{Name, Description, Fn, InputSchema}` + `AddTool` | `Name`/`Description` explicit (upstream used `__name__`/`__doc__`) |
| `Tool = Callable[..., Any]`           | `type Tool func(ctx, map[string]any) (any, error)` | |
| Python `get_public_callable` (signature stripping) | **dropped** — Go tools take an explicit args map, so there is no hidden ToolContext parameter to strip | gap §8 |

### `google.antigravity.triggers.triggers` → `trigger/`

| Python                                | Go (`trigger.*`)                            | Notes |
|---------------------------------------|---------------------------------------------|-------|
| `TriggerContext(connection)`          | `NewContext(notifier) *Context`             | `notifier` is a narrow local interface |
| `trigger_context.send(msg)`           | `(*Context).Send(ctx, content)`             | |
| `Trigger = AsyncCallable`             | `type Trigger func(ctx, *Context) error`    | |
| `trigger()` decorator                 | **dropped** — the function type is the contract | |

### `google.antigravity.triggers.trigger_runner` → `trigger/`

| Python                                | Go (`trigger.*`)                            | Notes |
|---------------------------------------|---------------------------------------------|-------|
| `TriggerRunner(connection)`           | `NewRunner(notifier) *Runner`               | |
| `register(name, fn)`                  | `Register(name, fn) (+ ErrRunning after Start)` | |
| `start(ctx)` / `stop()` / `is_running` | `Start(ctx)` / `Stop()` / `IsRunning()`    | reusable after Stop |

### `google.antigravity.triggers.helpers` → `trigger/`

| Python                                | Go (`trigger.*`)                            | Notes |
|---------------------------------------|---------------------------------------------|-------|
| `every(interval, callback)`           | `Every(interval, callback) Trigger`         | panics on non-positive interval |
| `on_file_change(path, callback)`      | `OnFileChange(path, callback) Trigger`      | Uses `github.com/fswatcher/fswatcher` (user-selected; non-recursive — gap §9). |

### `google.antigravity.mcp.bridge` → `mcp/`

| Python                                | Go (`mcp.*`)                                | Notes |
|---------------------------------------|---------------------------------------------|-------|
| `McpBridge()`                         | `NewBridge() *Bridge`                       | Built on the official `github.com/modelcontextprotocol/go-sdk` v1.x. |
| `bridge.connect(server_config)`       | `Connect(ctx, agtypes.McpServerConfig) error` | type-switches the sum type instead of reading `MCPType()` |
| `bridge.tools`                        | `Tools() []tool.ToolWithSchema`             | returns a copy |
| `bridge.stop()`                       | `Stop() error`                              | first close error wins |
| `get_mcp_tools(servers)` helper       | **inlined** as the unexported `toolsFromSession` — gap §10 |

### `google.antigravity.connections.connection` → `connection/`

| Python                                | Go (`connection.*`)                         | Notes |
|---------------------------------------|---------------------------------------------|-------|
| `AgentConfig` Pydantic model + `create_strategy` abstract method | `AgentConfig` interface (`CreateStrategy` + getters + setters + `Clone` + `Validate`) backed by embeddable `BaseAgentConfig` | The interface exposes the shared fields as methods. |
| `Connection` abstract class (default no-op methods) | `Connection` interface (every method) + `BaseConnection` struct providing no-op defaults for everything except `Send`/`ReceiveSteps`/`SendTriggerNotification` | |
| `ConnectionStrategy` async ctx mgr (`__aenter__` / `connect` / `__aexit__`) | `ConnectionStrategy` interface (`Start(ctx)` / `Connect() (Connection, error)` / `Close(ctx)`) + `ErrNotStarted` | |
| response_schema normalizer (dict/BaseModel/str → str) | `MarshalResponseSchema(any) (string, error)` | Pydantic.BaseModel branch dropped — gap §11 |
| test fakes                            | `FakeConnection` / `FakeStrategy` / `FakeConfig` (in `fake.go`, reusable by downstream tests) | non-test file by design |

### `google.antigravity.connections.local.local_connection` → `connection/local/`

| Python                                | Go (`local.*`)                              | Notes |
|---------------------------------------|---------------------------------------------|-------|
| `LocalConnection(process, ws, ...)`   | `LocalConnection` (embeds `connection.BaseConnection`) | constructed by `Strategy.Start` |
| `LocalConnectionStrategy(...)`        | `Strategy` + `StrategyConfig`               | `NewStrategy(cfg)` |
| `LocalConnectionStep` (extends Step)  | **dropped as a type** — extras (`cascade_id`/`trajectory_id`/`target`/`http_code`) carried in `Step.Extra` (Phase 2 design) | gap §12 |
| `normalize_wire_path`                 | `normalizeWirePath` (unexported)            | |
| `_extract_tool_result`                | `extractToolResult` (unexported) + `ToolOutput` interface |
| `_StepTracker`                        | `stepTracker` (unexported)                  | invariant: accessed only under the connection's mutex |
| `_get_default_binary_path` (env → resource → PATH) | `resolveBinaryPath` (env → PATH) + `HarnessPathEnv` + `ErrBinaryNotFound` | the Python package-resource branch has no Go analog — gap §13 |
| `callable_to_tool_proto`              | `toolProto` (unexported)                    | the genai `FunctionDeclaration` introspection branch dropped — gap §14 |

### `google.antigravity.connections.local.local_connection_config` → `connection/local/`

| Python                                | Go (`local.*`)                              | Notes |
|---------------------------------------|---------------------------------------------|-------|
| `LocalAgentConfig` Pydantic model     | `AgentConfig` (embeds `connection.BaseAgentConfig`) + `Model` / `APIKey` shorthand fields | |
| Pydantic validators                   | `(*AgentConfig).Validate() error` (idempotent; rejects shorthand conflicts; defaults Workspaces / Capabilities / Policies; absolute-AppDataDir check; **always prepends workspace_only policies**) | `Build()` is a thin wrapper that returns a validated copy. |
| `LocalAgentConfig.create_strategy`    | `CreateStrategy(toolRunner, hookRunner) (ConnectionStrategy, error)` | defaults SaveDir to a fresh temp dir |
|                                       | `DefaultAppDataDir()`                       | `$HOME/.gemini/antigravity` |

### `google.antigravity.connections.local.types` → `connection/local/`

| Python                                | Go (`local.*`)                              | Notes |
|---------------------------------------|---------------------------------------------|-------|
| `RunCommandResult` / `ListDirectoryEntry` / `ListDirectoryResult` / `SearchDirectoryResult` / `FindFileResult` / `EditFileResult` / `GenerateImageResult` / `TextResult` | same names; each with `String()`; over a sealed `ToolOutput` interface | snake_case `json:` tags for upstream wire parity when round-tripped |
| `ToolOutput` union                    | `ToolOutput` interface (sealed)             | |

### `google.antigravity.utils.interactive` → `interactive/`

| Python                                | Go (`interactive.*`)                        | Notes |
|---------------------------------------|---------------------------------------------|-------|
| `async_input(prompt)` (thread)        | `Prompter` interface + `StdinPrompter` / `NewPrompter(r, w)` / `NewStdinPrompter` | goroutine + ctx cancellation; testable |
| `ToolConfirmationHook` class          | `NewToolConfirmationHook(p) hook.PreToolCallDecide` | constructor returns the typed callback |
| `AskUserHandler` factory              | `AskUserHandler(p) policy.AskUserHandler`   | |
| `AskQuestionHook` class               | `NewAskQuestionHook(p) hook.OnInteraction`  | |
| `run_interactive_loop(agent)`         | `RunInteractiveLoop(ctx, agent, p) error`  | uses `ChatResponse.TextDeltas` for streaming output |
| internal `_upgrade_to_interactive_confirmation` (mutates private state) | `WithUserConfirmation(cfg, p) AgentConfig` (public, opt-in before `New`) | gap §1 |
| `ErrInputClosed`                      | (no Python counterpart)                     | Go needs a sentinel for end-of-input |

## Deliberate gaps and rationale

1. **`_upgrade_to_interactive_confirmation` not ported.** It mutated a started
   agent's private `_config.policies` and `_hook_runner._pre_tool_call_decide_hooks`.
   The Go-clean replacement is `interactive.WithUserConfirmation(cfg, p)`,
   applied *before* `New(ctx, cfg)`. Same effect, no private-state hack.
2. **`agtypes.PythonTool` deferred.** Python aliased it to `Callable[..., Any]`.
   The Go equivalent is `tool.Tool` / `tool.ToolWithSchema`. Aliasing it into
   `agtypes` would force `agtypes` to depend on `tool`, inverting the
   dependency direction.
3. **No implicit `string → ModelEntry` coercion.** Pydantic's
   `BeforeValidator` accepted a bare model-name string. Go requires the
   explicit constructor `agtypes.NewModelEntry(name)`. Documented; favors
   explicitness.
4. **`AntigravityValidationError.from_pydantic` dropped.** Tied to Pydantic
   validation errors. Wrap Go validation errors directly via `ValidationError.Err`.
5. **`Content` and `ContentPrimitive` are `any` aliases.** Go cannot express a
   closed union over a builtin (`string`) and an interface (`Media`). Documented
   to accept `string`, an `agtypes.Media` value, or a `[]ContentPrimitive`.
   The connection layer validates the dynamic type when translating to the wire.
6. **Hook generics not modeled.** Python's `InspectHook[T] | DecideHook[T] | TransformHook[T,R]`
   union didn't translate to Go's less flexible generics, and each concrete
   hook has one fixed data type anyway. The port uses 9 typed function values
   behind a sealed `Hook` marker interface; `Runner.Register(Hook)` dispatches
   by dynamic type. Behaviorally equivalent, simpler.
7. **`Predicate` always receives the full `ToolCall`.** Python inspected the
   predicate's first-parameter annotation to choose between passing args-dict,
   a typed Pydantic model, or the `ToolCall`. Go has no equivalent reflection;
   predicates read `tc.Args` themselves. Documented.
8. **`ToolContext` injection via `context.Context`, not signature inspection.**
   Python found a `ToolContext`-typed parameter and bound it; Go has no
   runtime parameter binding. `tool.Runner.Execute` calls `tool.WithToolContext(ctx, tc)`;
   a tool retrieves it via `tool.FromContext(ctx)`. This is also why
   `get_public_callable` (schema stripping for the hidden parameter) is not
   needed in Go.
9. **`OnFileChange` is non-recursive.** Python's `watchfiles.awatch`
   recurses; the Go `fswatcher` `Watcher.Add` watches direct entries only.
   Documented on the godoc as a deliberate divergence; users needing recursion
   can walk and add themselves.
10. **`get_mcp_tools` helper inlined.** The Python free function added no
    capability beyond `Bridge.Tools()`; folded into the bridge.
11. **`MarshalResponseSchema` drops the Pydantic.BaseModel branch.** No Go
    equivalent of subclassing a serializer model. Accepts a JSON string
    (validated) or any value that marshals to JSON.
12. **`LocalConnectionStep` collapsed into `Step.Extra`.** The
    extension fields (`cascade_id`, `trajectory_id`, `target`, `http_code`)
    are carried in `Step.Extra` (the Phase 2 design provided for this), so
    `Connection.ReceiveSteps` returns `iter.Seq2[agtypes.Step, error]`
    without a connection-specific subtype.
13. **Binary resolution drops the package-resource branch.** Python's
    `importlib.resources.files("google.antigravity")/"bin/localharness"` has
    no Go analog (Go has no concept of bundling per-platform binaries in a
    module). Resolution order is env → PATH only.
14. **`callable_to_tool_proto` drops the genai introspection path.** Upstream
    fed a bare callable through `genai.FunctionDeclaration.from_callable_with_api_option`
    to derive a schema. Go tools are always `tool.ToolWithSchema` carrying an
    explicit schema, so the introspection path is dead weight in Go and the
    `google.golang.org/genai` dependency is not pulled in here.
15. **Wire-fidelity testing.** The `localharness` binary is a vendored
    pre-compiled artifact not in the upstream repo, and we did not have one
    during the port. Wire-shape coverage is split across three layers:
    - **Unit tests** (`connection/local/*_test.go`) build proto fixtures from
      the generated builders and exercise pure logic (`stepFromUpdate`,
      `extractToolResult`, `stepTracker`, framing, policy/question handlers).
    - **In-process integration tests** (`connection/local/wire_test.go`) drive
      the real reader loop end-to-end over a `fakeWS` that shuttles protojson
      bytes between the SDK and a simulated harness, covering the upstream
      `test_utils.py` / `local_connection_test.py` integration scenarios
      (step routing, tool_confirmation flow with pending-builtin tracking,
      questions_request, wait-state dedup + state-transition reset, subagent
      parent/child idle accounting, host tool_call execution, file:// URI
      normalization). The fake shares the production protojson encoder, so
      the wire format under test is the wire format the binary speaks.
    - **Ground-truth integration** (`connection/local/integration_test.go`)
      is the only test that spawns the real harness. It is gated on
      `ANTIGRAVITY_HARNESS_PATH` or a `localharness` binary on `$PATH` (plus
      `GEMINI_API_KEY`) and skips otherwise. This is the remaining gap: when
      a binary is available, run this test to catch any drift between the
      port's protojson encoding and the harness's actual wire shape.

## Renames

1. **`McpStreamableHttpServer` → `McpStreamableHTTPServer`** (and
   `MCPType()`, `MCPServers()`). Go convention is uppercase initialism;
   matches `net/http`, `aws-sdk-go-v2`, etc.
2. **`SessionContext` / `TurnContext` / `OperationContext` → `hook.Context`**
   with constructors `NewSessionContext` / `NewTurnContext(parent)` /
   `NewOperationContext(parent)`. One type, scope determined by construction.
3. **`policy.deny` → `policy.DenyTool`** to avoid colliding with the
   `policy.Deny` `Decision` const.

## What's NOT in the Go port (intentional)

- The genai `FunctionDeclaration` introspection path (see gap §14).
- The `importlib.resources` binary lookup (see gap §13).
- The `_upgrade_to_interactive_confirmation` private-state hack (see gap §1).
- Pydantic-specific machinery: `from_pydantic`, `BeforeValidator` coercion,
  `model_validator` reflection — all replaced with explicit constructors,
  `Validate()` methods, or documented field types.
- The Python "decorators" pattern for hooks and triggers; the Go function
  types are the contract.

## Module-level summary

| Python module                                | Go package                  | Status |
|----------------------------------------------|-----------------------------|--------|
| `google.antigravity` (`__init__`)            | `antigravity` (root facade) | ✓ |
| `google.antigravity.agent`                   | `antigravity` (root)        | ✓ |
| `google.antigravity.types`                   | `agtypes`                   | ✓ |
| `google.antigravity.conversation.conversation` | `antigravity` (root)      | ✓ |
| `google.antigravity.hooks.hooks` / `hook_runner` | `hook`                  | ✓ |
| `google.antigravity.hooks.policy`            | `hook/policy`               | ✓ |
| `google.antigravity.tools.tool_context` / `tool_runner` | `tool`           | ✓ |
| `google.antigravity.triggers.{triggers,trigger_runner,helpers}` | `trigger` | ✓ |
| `google.antigravity.mcp.bridge`              | `mcp`                       | ✓ |
| `google.antigravity.connections.connection`  | `connection`                | ✓ |
| `google.antigravity.connections.local.{local_connection,local_connection_config,types}` | `connection/local` | ✓ |
| `google.antigravity.utils.interactive`       | `interactive`               | ✓ |
| upstream proto descriptor (`localharness_pb2.py`) | `internal/localharnesspb` | ✓ generated from the embedded `FileDescriptorProto`; source of truth is the checked-in `localharness.fds` |
| (no upstream equivalent)                     | `connection/local/types.go` ToolOutput sum type | ported into `local/` rather than a separate package |

## How to keep this in sync

- Track upstream `main`. Re-run the audit when upstream tags a release or the
  port targets a new SHA.
- The `port-from-python` skill compares the upstream module against its Go
  counterpart and reports diffs.
