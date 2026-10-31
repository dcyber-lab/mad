import MadKit
import SwiftUI

/// An agent's conversation, drawn from its transcript.
struct ChatScreen: View {
    @ObservedObject var mac: MacModel
    @StateObject private var chat: ChatSession
    @Environment(\.scenePhase) private var scenePhase
    @State private var draft = ""

    let agentID: String

    init(mac: MacModel, agentID: String) {
        self.mac = mac
        self.agentID = agentID
        _chat = StateObject(wrappedValue: ChatSession(agentID: agentID, api: mac.client,
                                                      sample: mac.sampleChat(for: agentID)))
    }

    private var agent: DeckSnapshot.Agent? { mac.snapshot?.agent(id: agentID) }
    private var working: Bool {
        guard let a = agent, let s = mac.snapshot else { return false }
        return s.tone(for: a.state) == "working"
    }

    var body: some View {
        VStack(spacing: 0) {
            if let p = chat.problem {
                ChatProblem(text: p) { Task { await chat.look() } }
            }
            transcript
        }
        .safeAreaInset(edge: .bottom, spacing: 0) { composer }
        .navigationTitle(agent?.label ?? agentID)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if let a = agent, let s = mac.snapshot {
                ToolbarItem(placement: .principal) {
                    VStack(spacing: 0) {
                        Text(a.label).font(.headline)
                        Text(s.word(for: a.state)).font(.caption2).foregroundStyle(Tone.color(s.tone(for: a.state)))
                    }
                }
            }
        }
        // Reads while this screen is shown and the app is active.
        .task(id: scenePhase) {
            guard scenePhase == .active else { return }
            await chat.run()
        }
        .alert("mad", isPresented: Binding(get: { chat.notice != nil }, set: { if !$0 { chat.notice = nil } })) {
            Button("OK", role: .cancel) {}
        } message: { Text(chat.notice ?? "") }
    }

    private var transcript: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 14) {
                if chat.transcript.earlier {
                    Text("Earlier messages are not shown.").font(.caption).foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity)
                }
                ForEach(Array(chat.transcript.rows.enumerated()), id: \.offset) { _, row in
                    RowView(row: row)
                }
                if let m = chat.live.visible(in: chat.transcript) {
                    LiveBubble(message: m)
                }
                if chat.loaded && chat.transcript.entries.isEmpty && chat.live.message == nil {
                    Text("Nothing here yet.").foregroundStyle(.secondary).frame(maxWidth: .infinity)
                        .padding(.top, 40)
                }
            }
            .padding(.horizontal, 16).padding(.vertical, 12)
        }
        .defaultScrollAnchor(.bottom)
        .scrollDismissesKeyboard(.interactively)
        .overlay { if !chat.loaded && chat.problem == nil { ProgressView() } }
    }

    @ViewBuilder private var composer: some View {
        VStack(spacing: 0) {
            Divider()
            if working {
                HStack {
                    ProgressView().controlSize(.small)
                    Text("Working").font(.footnote).foregroundStyle(.secondary)
                    Spacer()
                    Button("Interrupt", systemImage: "stop.circle") {
                        Task {
                            await chat.interrupt()
                            UINotificationFeedbackGenerator().notificationOccurred(chat.notice == nil ? .success : .error)
                        }
                    }
                        .font(.footnote)
                }
                .padding(.horizontal, 16).padding(.top, 8)
            }
            if agent?.headless == false {
                Text("This agent runs in its terminal. Open the Deck tab to talk to it.")
                    .font(.footnote).foregroundStyle(.secondary).padding(12)
            } else {
                HStack(alignment: .bottom, spacing: 8) {
                    TextField("Message", text: $draft, axis: .vertical)
                        .lineLimit(1...5)
                        .padding(.horizontal, 12).padding(.vertical, 8)
                        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 18))
                    Button {
                        let sent = draft
                        Task {
                            let ok = await chat.send(sent)
                            UINotificationFeedbackGenerator().notificationOccurred(ok ? .success : .error)
                            // Only what went out is cleared: the next message typed meanwhile stays.
                            if ok { draft = ChatSession.draftAfterSend(sent: sent, current: draft) }
                        }
                    } label: {
                        Image(systemName: "arrow.up.circle.fill").font(.system(size: 32))
                    }
                    .disabled(draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || chat.sending)
                }
                .padding(.horizontal, 12).padding(.vertical, 8)
            }
        }
        .background(.bar)
    }
}

/// Why the conversation cannot be read, with a way to try again.
struct ChatProblem: View {
    let text: String
    let retry: () -> Void

    var body: some View {
        HStack {
            Label(text, systemImage: "exclamationmark.triangle").font(.footnote).foregroundStyle(.orange)
            Spacer()
            Button("Retry", action: retry).font(.footnote.weight(.semibold))
        }
        .padding(.horizontal, 12).padding(.vertical, 8)
        .background(Color.orange.opacity(0.15))
    }
}

/// One row of the conversation.
struct RowView: View {
    let row: ChatRow

    var body: some View {
        switch row {
        case .user(let e):
            HStack {
                Spacer(minLength: 48)
                VStack(alignment: .trailing, spacing: 4) {
                    if let t = e.text, !t.isEmpty { Markdown(text: t).foregroundStyle(.white) }
                    if !e.images.isEmpty { ImagePlaceholder(count: e.images.count).foregroundStyle(.white.opacity(0.85)) }
                }
                .padding(.horizontal, 12).padding(.vertical, 8)
                .background(Color.accentColor, in: RoundedRectangle(cornerRadius: 18))
            }
        case .text(let e):
            VStack(alignment: .leading, spacing: 4) {
                if let t = e.text, !t.isEmpty { Markdown(text: t) }
                if !e.images.isEmpty { ImagePlaceholder(count: e.images.count).foregroundStyle(.secondary) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        case .tool(let call, let result):
            ToolRow(call: call, result: result)
        }
    }
}

/// Inline markdown (bold, italics, code, links), as the page's text does.
struct Markdown: View {
    let text: String

    var body: some View {
        if let a = try? AttributedString(markdown: text, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)) {
            Text(a).textSelection(.enabled)
        } else {
            Text(text).textSelection(.enabled)
        }
    }
}

struct ImagePlaceholder: View {
    let count: Int
    var body: some View {
        Label(count == 1 ? "Image" : "\(count) images", systemImage: "photo").font(.footnote)
    }
}

/// A call as one compact row, "Tool · label"; it opens to the input and, folded
/// under it, the result (red when it failed).
struct ToolRow: View {
    let call: ChatEntry
    let result: ChatEntry?
    @State private var open = ToolRow.startOpen

    /// Debug builds only: `-expandTools YES` starts every call open.
    private static var startOpen: Bool {
        #if DEBUG
        return UserDefaults.standard.bool(forKey: "expandTools")
        #else
        return false
        #endif
    }

    private var failed: Bool { result?.error == true }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button { withAnimation(.easeInOut(duration: 0.15)) { open.toggle() } } label: {
                HStack(spacing: 6) {
                    Image(systemName: "chevron.right").font(.caption2.weight(.semibold))
                        .rotationEffect(.degrees(open ? 90 : 0))
                    Text(title).font(.footnote.weight(.medium)).lineLimit(1)
                    if failed { Circle().fill(.red).frame(width: 6, height: 6) }
                    else if result == nil { ProgressView().controlSize(.mini) }
                    Spacer(minLength: 0)
                }
                .foregroundStyle(failed ? Color.red : Color.secondary)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if open {
                VStack(alignment: .leading, spacing: 8) {
                    if let i = call.input, !i.isEmpty { Mono(text: i, color: .primary) }
                    if let r = result {
                        if let t = r.text, !t.isEmpty { Mono(text: t, color: r.error ? .red : .secondary) }
                        if !r.images.isEmpty { ImagePlaceholder(count: r.images.count).foregroundStyle(.secondary) }
                    }
                }
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 10))
            }
        }
    }

    private var title: String {
        let tool = call.tool ?? "Tool"
        if let l = call.label, !l.isEmpty { return "\(tool) · \(l)" }
        return tool
    }
}

struct Mono: View {
    let text: String
    let color: Color
    var body: some View {
        Text(text).font(.system(.caption, design: .monospaced)).foregroundStyle(color)
            .textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// The message being written, until the transcript has it.
struct LiveBubble: View {
    let message: LiveMessage

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(Array(message.blocks.enumerated()), id: \.offset) { _, b in
                switch b.kind {
                case "text":
                    if let t = b.text, !t.isEmpty { Markdown(text: t) }
                case "thinking":
                    Text("Thinking…").font(.footnote).italic().foregroundStyle(.secondary)
                case "tool":
                    Label(b.tool ?? "Tool", systemImage: "wrench.and.screwdriver").font(.footnote).foregroundStyle(.secondary)
                default:
                    EmptyView()
                }
            }
            if !message.done { ProgressView().controlSize(.small) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// The colors of the deck's tones.
enum Tone {
    static func color(_ tone: String) -> Color {
        switch tone {
        case "working": return .green
        case "needs": return .orange
        case "finished": return .blue
        case "error": return .red
        default: return .secondary
        }
    }
}
