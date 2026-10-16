# Proposal: an agent runtime with thread semantics and file communication

Status: proposed. This document describes a direction for mad; the interfaces
and directory layout below are proposals, not features available today.

## Philosophy

Treat each agent as a logical operating-system thread: an execution unit with
a stable identity, a lifecycle, a workspace, and the ability to wait for other
work. Use files to exchange requests, answers, and artifacts. Let mad manage
execution, delivery, waiting, and recovery.

The thread analogy describes the interface. Each agent still runs as an
independent process with its own conversation and model context. Shared files
provide the common workspace that threads would otherwise access through shared
memory. Scheduling happens at task and conversation-turn boundaries, rather
than by preempting model execution.

The resulting contract should be simple enough for an agent, a shell script,
or a person to use without a provider-specific messaging service.

## Design principles

1. **Stable identity.** An agent's logical thread survives pane changes,
   process restarts, and conversation compaction. Thread IDs, provider session
   IDs, task IDs, and execution-attempt IDs have separate meanings.
2. **Communication persists.** Requests, replies, and completion records are
   published as files before dependent work advances. A terminal notification
   can wake a reader, but the files remain the source of truth.
3. **The runtime owns execution state.** Agents submit requests and produce
   artifacts. mad validates requests and records lifecycle transitions.
4. **Published artifacts have identity.** A consumer can identify the version
   it read, and a reviewer can identify the exact code it reviewed.
5. **Recovery follows evidence.** Restarting mad reconciles durable records
   with the running backend and its outputs before deciding to wait or retry.

## Existing foundations and gaps

mad already provides much of the machinery this design needs:

| Area | Existing implementation | Next improvement |
| --- | --- | --- |
| Identity and lifecycle | [Agent IDs and session IDs](../internal/state/state.go), start/resume/sleep through `internal/deck` | Define lifecycle independently of panes and tasks |
| Shared work | A worktree per run, design files, step replies, and `run.json` | Define versioned inputs and outputs for every task |
| File communication | [User-note inbox](../internal/run/notes.go) and file commands | Generalize into addressed messages with acknowledgements |
| Delivery and waiting | [tmux paste, hooks, and turn reports](../internal/drive/drive.go) | Associate delivery and completion with task and attempt IDs |
| Recovery | Saved progress and [one runner lock per run](../internal/run/exec.go) | Reconcile uncertain delivery and partially completed attempts |
| Scheduling | Ordered flow steps with review loops | Add dependency-based readiness, joins, and global limits |

Some communication still travels through conversation text: the runner
forwards questions and answers, and parses `DONE`, `QUESTION`, and `VERDICT`
markers. A turn report stores the latest reply for an agent; it is not a
complete task-result history. The sidebar also infers some activity from
screen changes. These mechanisms can remain backend adapters while the
runtime gains a durable, provider-independent protocol.

## Execution model

| Object | Meaning | Important fields |
| --- | --- | --- |
| Thread | A persistent agent execution unit | ID, optional parent, provider, session, workspace, lifecycle |
| Task | One piece of work assigned to a thread | ID, input references, dependencies, execution policy, result |
| Attempt | One execution of a task | ID, task ID, thread generation, acceptance, outcome |
| Message | A request, answer, or event addressed to a thread | ID, sender, recipient, correlation ID, content references |
| Artifact | A published output consumed by another task | ID or path, producing task, version or content hash |

A thread can process many tasks over its lifetime. A task completing does not
imply that its thread exits. A restarted provider process gets a new execution
generation while retaining the logical thread ID, so a late completion from
an older process cannot silently complete a newer attempt.

Start with `ready`, `running`, `blocked`, `sleeping`, and `exited` thread
states. A blocked thread names its reason and wait target: a message, another
task, user input, or a resource limit. Keep task outcomes separate, such as
`succeeded`, `failed`, and `cancelled`. A provider process exiting unexpectedly
does not by itself prove that its task failed or that its output is absent.

Expose these operations through the CLI and the file protocol: create a
thread, submit work, receive a message, wait for a task, join an exited thread,
and request cancellation. Existing `mad spawn`, `mad send`, and `mad wait`
should retain their current behavior during migration; task-specific commands
can be added without changing what existing scripts wait for.

## File communication protocol

Use a runtime namespace shared by a project's threads, separate from their
individual source worktrees. Resolve it using the deck and repository identity,
and expose its absolute path as `MAD_IPC_DIR`. It can live under mad's existing
state directory. Separate decks must have separate runtime namespaces.

A candidate layout is:

```text
$MAD_IPC_DIR/
  threads/<thread-id>/
    meta.json
    inbox/<message-id>.json
    outbox/<message-id>.json
  tasks/<task-id>/
    request.json
    attempts/<attempt-id>/
      accepted.json
      result.json
  artifacts/<artifact-id>/
  events/<sequence>.json
```

Agents publish requests in their own outbox. The runtime validates and routes
them to the recipient's inbox. Runtime records such as thread metadata,
acceptance, and task outcomes have one designated runtime writer. Provider
adapters turn completion reports into these records; agents do not rewrite
another thread's lifecycle state.

Keep machine-readable routing and outcomes in JSON, and readable work in
Markdown or the artifact's native format. Messages can refer to large files
instead of repeating their contents in every conversation. For example:

```json
{
  "version": 1,
  "id": "message-17",
  "from": "builder",
  "to": "designer",
  "type": "request",
  "task_id": "task-42",
  "reply_to": null,
  "body": "Should retries preserve the original request ID?",
  "artifacts": ["artifacts/design-v1/design.md"]
}
```

The answer gets a new message ID and refers to `message-17` through `reply_to`.
The sender, recipient, task, and artifact references are checked against the
runtime registry. IDs in this example are illustrative; production IDs must
be unique within the namespace. Runtime-issued sequence numbers can establish
ordering where needed; wall-clock timestamps should not determine causality.

Publishing a file means writing it completely to a temporary file in the
destination filesystem and then renaming it into place. Published messages
are immutable. Delivery, acknowledgement, and archival are separate runtime
records rather than arbitrary edits by multiple participants. Each thread
has one dispatcher, so concurrent senders cannot paste overlapping tasks
into the same provider prompt.

The first protocol version should target local filesystems. Filesystem
notifications are wake-up hints, with periodic reconciliation for missed
events. Preserve existing sockets where useful for UI wake-ups; they need not
carry agent messages. Stronger durability against machine or power failure
requires syncing records and their parent directories, beyond atomic rename.

## Reliable delivery and recovery

Use at-least-once delivery with stable IDs and deduplication. Do not promise
exactly-once execution across file writes, terminal input, and external side
effects.

The current runner sends a prompt and then saves its `Sent` flag. A crash
between those operations leaves an uncertain delivery. Likewise, writing all
control commands to one `cmd` file can replace an earlier command before it
is read. Address both with per-request records and explicit progress:

1. Persist the request before attempting delivery.
2. Record the attempt and its claim under the runtime's dispatcher ownership.
3. Record backend acceptance when there is evidence the agent took the task.
4. Publish the result and artifact references before releasing dependents.
5. On restart, reconcile records with the backend session and outputs.

Acceptance is not completion. A crash after a commit or another external
action can leave valid work without a final result record. Retry automatically
only when the operation is safe to repeat or completion can be reconciled.
Otherwise mark the attempt uncertain and request intervention. Task policy
must describe retry limits and how to identify prior side effects.

Keep an ordered runtime event journal alongside state snapshots so transitions
can be inspected and replayed. Use the existing file-lock approach to ensure
one supervisor owns each namespace. Per-task records should retain failures
and intermediate outcomes rather than overwrite the last result.

## Scheduling and resource limits

Begin with cooperative scheduling. A thread that needs an answer publishes a
request and yields at a turn boundary. mad records the wait and schedules
other ready tasks. An answer makes the waiting task eligible to resume; the
model does not need to spend tokens polling a directory.

Retain ordered flows as a supported policy. Add dependencies and joins when
the underlying task protocol is stable: independent tasks can run together,
and a dependent task becomes ready once its required inputs are successfully
published. Review loops create explicit new attempts against identified input
versions. Detect dependency cycles and report wait chains to the user.

Enforce concurrency and provider limits across runs, not only within each
run. Combine per-task timeouts and retry limits with project or deck budgets,
queue backpressure, and provider rate limits. Resource estimates should state
whether they measure tokens, API-equivalent cost, or actual process memory.
Reuse idle threads and consider starting roles on demand, while still giving
users a way to settle provider trust and permission prompts before execution.

Cancellation is a recorded request. It propagates to child tasks according to
policy, reaches the backend, and records whether execution actually stopped.
Writing a cancellation file alone must not be reported as successful process
termination. Sleep or resume should occur at safe boundaries with durable
inputs and an identifiable provider session.

## Workspace ownership and artifact versions

Sharing a filesystem requires explicit write ownership:

- A sequential run can continue sharing its existing worktree.
- A shared source worktree has one active code writer by default. Concurrent
  implementers use separate worktrees and exchange commits or patches.
- Published artifacts are immutable versions. Consumers name the version
  they used; an update creates a new version.
- A reviewer receives a stable commit or snapshot, including any work intended
  for review that has not been committed.
- Agents receive access to their communication directories and the workspaces
  their tasks need. Runtime state remains under runtime ownership.

These rules describe coordination. Enforcement depends on provider sandboxes
or OS isolation; directory conventions alone do not isolate processes running
as the same user. Extend the existing worktree checks and sandbox configuration
to grant the communication paths each role actually requires.

## Runtime, backends, and interface

Separate supervision and scheduling from panel rendering. The runtime owns
thread and task records; the TUI reads those records and submits commands.
Backend adapters own provider launch, task delivery, completion observation,
resume, and cancellation. tmux remains a useful interactive backend and view.

Hooks and transcript parsing can populate runtime records. Screen inference
can still help display activity, but it should not establish task success.
The file protocol also gives other agents and shell workers a completion
interface without requiring Claude- or Codex-specific transcript support.

Preserve incremental transcript reads and batched screen captures. As thread
counts grow, watch communication directories, reconcile periodically, avoid
duplicate per-run polling, and archive completed messages and results using
a retention policy that preserves referenced artifacts.

## Incremental implementation

| Phase | Scope | Completion criteria |
| --- | --- | --- |
| 1: identities and file IPC | Define thread/task/attempt records; implement atomic request publication, routing, acknowledgements, and results | Two scripted workers exchange a request and answer through files with correct correlation |
| 2: recovery and adapters | Add ownership, deduplication, reconciliation, and backend adapters; migrate one existing run flow | Restart at delivery and completion boundaries without confusing attempts or blindly repeating uncertain work |
| 3: scheduling and ownership | Add dependencies, joins, global limits, cancellation propagation, and workspace rules | Independent work runs concurrently; waiting work consumes no model turns; code writers and reviews have stable inputs |
| 4: interface and scale | Separate panel rendering, expose runtime operations in the CLI/TUI, and tune event handling and retention | Run work without an attached UI and observe the same durable state when the UI returns |

The first implementation should keep existing commands and flows operational.
Use scripted workers to validate the runtime without spending model tokens,
then verify a real provider adapter before migrating all runs.

The essential checks are concurrent publication without partial reads or lost
messages; duplicate delivery and stale completion handling; crash recovery
before and after backend acceptance; uncertainty after external side effects;
cancellation during execution; missed filesystem notifications; dependency
cycles; and a waiting thread resuming on the correct answer. Performance
checks should measure overhead as idle and active thread counts grow.

Before freezing the protocol, settle the exact schemas and CLI names, the
supported filesystem durability level, artifact retention, and how each
provider proves acceptance and completion. Those decisions should preserve
the core contract: stable execution identities, file communication, explicit
ownership, versioned outputs, and recovery based on durable evidence.
