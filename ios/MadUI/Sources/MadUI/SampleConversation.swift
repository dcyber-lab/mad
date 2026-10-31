#if DEBUG
import SwiftUI

/// A conversation that shows everything the screen draws, for the gallery and previews.
public enum SampleConversation {
    public static let model = AgentConversation(
        title: "给我绘制一个表格",
        agentID: "cat00001-aaaa",
        kind: "claude",
        state: .needsYou,
        entries: [
            .earlier(),
            .user("u1", "给我绘制一个表格，把本周的任务汇总一下"),
            .text("a1", "好的，我先读一下任务板，再生成表格。\n\n- 读 `board.json`\n- 按负责人分组\n- 写成 **report.xlsx**"),
            .tool("t1", ConvTool(name: "Read", label: "board.json", input: "board.json",
                                 result: "{\n  \"tasks\": 15,\n  \"done\": 12,\n  \"owners\": [\"architect\", \"builder\", \"reviewer\", \"ios\"]\n}")),
            .tool("t2", ConvTool(name: "Bash", label: "go test ./internal/board/...", input: "go test ./internal/board/...",
                                 result: "--- FAIL: TestSummary (0.01s)\n    summary_test.go:42: want 15 tasks, got 14\nFAIL\nFAIL\tgithub.com/dcyber-lab/mad/internal/board\t0.318s", error: true)),
            .text("a2", "测试没过，少了一条任务。看了一下，原因在这里：\n\nhttps://github.com/dcyber-lab/mad/blob/main/internal/board/summary.go#L42-L120?plain=1&token=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n\n```go\nfunc Summary(tasks []Task) map[string][]Task {\n    out := map[string][]Task{}\n    for _, t := range tasks[1:] { // skips the first task\n        out[t.Owner] = append(out[t.Owner], t)\n    }\n    return out\n}\n```\n\n循环从第二条开始，所以第一条任务被漏掉了。这是一段很长很长很长很长很长很长很长很长很长很长很长很长的中文，用来看它会不会在屏幕边上正确换行。"),
            .user("u2", "那就改成从头开始", images: [ConvImage(alt: "screenshot.png")]),
            .tool("t3", ConvTool(name: "Edit", label: "internal/board/summary.go", input: "- for _, t := range tasks[1:] {\n+ for _, t := range tasks {", result: nil)),
        ],
        live: LiveBubble(markdown: "改好了，再跑一遍测试"),
        ask: AskCard(id: "ask1", title: "想运行一条命令", detail: "python3 make_table.py --out report.xlsx --owners architect,builder,reviewer,ios --week 2026-W44", mono: true,
                     acts: [AskAct(id: "allow", label: "允许", primary: true), AskAct(id: "always", label: "总是允许"), AskAct(id: "deny", label: "拒绝")]))

    /// The same, with a question of its own to answer instead.
    public static let asking = AgentConversation(
        title: "ios", agentID: "ios00001-bbbb", kind: "codex", state: .needsYou, entries: Array(model.entries.prefix(3)),
        ask: AskCard(id: "ask2", title: "用哪种图表？", acts: [
            AskAct(id: "bar", label: "柱状图", primary: true), AskAct(id: "line", label: "折线图"), AskAct(id: "other", label: "自己写…", input: true)]))

    /// Stopped: it can't take messages.
    public static let stopped = AgentConversation(
        title: "输入法切换", agentID: "ime00001-iiii", kind: "claude", state: .stopped, entries: Array(model.entries.prefix(3)),
        canSend: false, sendHint: "这个 agent 已停止，在 Mac 上重新启动后才能发消息")
}

/// The phone's screen, the Mac's detail pane and the two other cases, one at a time.
public struct ConversationGallery: View {
    public enum Case: String, CaseIterable { case ask, question, stopped }

    @State private var which: Case
    public var detail: Bool

    public init(which: Case = .ask, detail: Bool = false) {
        _which = State(initialValue: which)
        self.detail = detail
    }

    var model: AgentConversation {
        switch which {
        case .ask: SampleConversation.model
        case .question: SampleConversation.asking
        case .stopped: SampleConversation.stopped
        }
    }

    public var body: some View {
        VStack(spacing: 0) {
            Picker("", selection: $which) { ForEach(Case.allCases, id: \.self) { Text($0.rawValue).tag($0) } }
                .pickerStyle(.segmented)
                .padding(8)
            ConversationView(model: model, showsBack: !detail)
                .frame(maxWidth: detail ? 720 : .infinity)
        }
    }
}

#Preview("Conversation") { ConversationGallery() }
#endif
