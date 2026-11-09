import MadKit
import MadUI
import SwiftUI

/// 新对话, on the Mac in the window's detail pane and on the iPhone as a screen over the home list (the page's new chat, natively): 想做点什么？, a box to write the first
/// message in with the project, the kind and the model under it, and the project's earlier sessions to pick up.
/// Sending starts an agent with that message and opens it.
/// An agent started from 新对话: its id, the first message, and why that did not go (nil when it went).
struct StartedChat: Equatable {
    var id: String
    var text: String
    var failure: String?
    /// When it was started.
    var at = Date()
}

/// What a started agent's page is handed when it opens, by agent id: a first message that did not go (the composer's
/// draft) and why (a note at the top of its page) are given once; a first message that went is its bubble until the
/// transcript has it.
struct Handovers: Equatable {
    var drafts: [String: String] = [:]
    var failures: [String: String] = [:]
    var firsts: [String: PendingFirst] = [:]

    mutating func add(_ chat: StartedChat) {
        let wrote = !chat.text.isEmpty
        drafts[chat.id] = chat.failure != nil && wrote ? chat.text : nil
        failures[chat.id] = chat.failure
        firsts[chat.id] = chat.failure == nil && wrote ? PendingFirst(text: chat.text, since: chat.at) : nil
    }

    /// The page took its draft and note: reopening it does not bring them back (the bubble stays, it gives way by itself).
    mutating func took(_ id: String) {
        drafts[id] = nil
        failures[id] = nil
    }
}

struct NewChatDetail: View {
    let api: NewChatAPI
    let projects: [Project]
    /// Opens the agent that was started (after the deck has it).
    let started: (StartedChat) async -> Void
    /// The iPhone's way back to the list (it draws its own head); the Mac's detail pane has none.
    var onBack: (() -> Void)?

    #if os(macOS)
    private static let top: CGFloat = 72, side: CGFloat = 32
    #else
    private static let top: CGFloat = 28, side: CGFloat = 18
    #endif

    static let kinds: [(id: String, name: String)] = [("claude", "Claude"), ("codex", "Codex"), ("pi", "Pi")]

    @AppStorage("mad.newchat.project") private var projectPath = ""
    @AppStorage("mad.kind") private var kind = "claude"
    @State private var model = ""
    @State private var models: [ModelChoice] = []
    @State private var past: [PastSession] = []
    @State private var text = ""
    @State private var sending = false
    @State private var error: String?
    @FocusState private var focused: Bool

    private var project: Project? { projects.first { $0.path == projectPath } ?? projects.first }

    private var composerFont: Font { .system(size: 16.5) } // composerFont, which the phone has not

    var body: some View {
        VStack(spacing: 0) {
            #if os(iOS)
            head
            #endif
            page
        }
        .background(Color.madPaper)
    }

    #if os(iOS)
    private var head: some View {
        HStack {
            Button { onBack?() } label: {
                Image(systemName: "chevron.backward").font(.system(size: 19, weight: .medium)).foregroundStyle(Color.madFg)
                    .frame(width: Phone.button, height: Phone.button)
            }
            .accessibilityLabel("返回")
            .accessibilityIdentifier("newchat.back")
            Spacer()
        }
        .padding(.horizontal, 8)
    }
    #endif

    private var page: some View {
        ScrollView {
            VStack(spacing: 0) {
                if let p = project {
                    Text(L("想做点什么？")).font(.system(size: 30, weight: .bold)).tracking(-0.4).foregroundStyle(Color.madFg)
                    Text(Lf("在 %@ 里", where_(p))).font(.system(size: 16.5)).foregroundStyle(Color.madFg3).padding(.top, 8)
                    composer(p).padding(.top, 28)
                    if let error { Text(error).font(.system(size: 14.5)).foregroundStyle(Color.madDanger).padding(.top, 10) }
                    if !past.isEmpty { earlier(p).padding(.top, 36) }
                } else {
                    Text(L("先添加一个项目")).font(.system(size: 30, weight: .bold)).foregroundStyle(Color.madFg)
                    Text(L("项目就是 agent 工作所在的目录。")).font(.system(size: 16.5)).foregroundStyle(Color.madFg3).padding(.top, 8)
                }
            }
            .padding(.top, Self.top)
            .padding(.bottom, 40)
            .frame(maxWidth: 760)
            .padding(.horizontal, Self.side)
            .frame(maxWidth: .infinity)
        }
        // the whole pane: a scroll view alone in the detail takes only its content's width
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .task(id: project?.path) { await loadHistory() }
        .task(id: "\(project?.path ?? "")|\(kind)") { await loadModels() }
        .onAppear { focused = true }
    }

    private func where_(_ p: Project) -> String {
        if let b = p.branch, !b.isEmpty { return p.name + " · " + b }
        return p.name
    }

    // MARK: the box

    private func composer(_ p: Project) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            ZStack(alignment: .topLeading) {
                if text.isEmpty {
                    Text(L("问点什么，或者交代一件事")).font(composerFont).foregroundStyle(Color.madFg3)
                        .padding(.top, 1).padding(.leading, 5).allowsHitTesting(false)
                }
                TextEditor(text: $text)
                    .accessibilityIdentifier("newchat.text")
                    .font(composerFont)
                    .scrollContentBackground(.hidden)
                    .frame(minHeight: 48, maxHeight: 220)
                    .fixedSize(horizontal: false, vertical: true)
                    .focused($focused)
                    #if os(macOS)
                    // ⏎ sends, ⇧⏎ starts a new line (as the page and the chats do)
                    .onKeyPress(.return, phases: .down) { press in
                        if press.modifiers.contains(.shift) { return .ignored }
                        send(p)
                        return .handled
                    }
                    #endif
            }
            chips(p)
        }
        .padding(.horizontal, 16)
        .padding(.top, 16)
        .padding(.bottom, 12)
        .background(Color.madPaper, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 22, style: .continuous).strokeBorder(Color.madLine, lineWidth: 1))
        .shadow(color: .black.opacity(0.06), radius: 14, y: 6)
    }

    /// The project, the kind and the model, and the send button; on the phone the send button has a line to itself.
    @ViewBuilder private func chips(_ p: Project) -> some View {
        #if os(macOS)
        HStack(spacing: 8) { projectMenu(p); kindPicker; modelMenu; Spacer(minLength: 8); sendButton(p) }
        #else
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) { projectMenu(p); modelMenu; Spacer(minLength: 8) }
            HStack(spacing: 8) { kindPicker; Spacer(minLength: 8); sendButton(p) }
        }
        #endif
    }

    private func sendButton(_ p: Project) -> some View {
        Button { send(p) } label: {
            Image(systemName: "arrow.up").font(.system(size: 15, weight: .semibold))
                .foregroundStyle(canSend ? Color.madPaper : Color.madFg3)
                .frame(width: 34, height: 34)
                .background(canSend ? Color.madFg : Color.madRaise, in: Circle())
        }
        .buttonStyle(.plain)
        .disabled(!canSend)
        .help(L("发送"))
        .accessibilityIdentifier("newchat.send")
    }

    private var canSend: Bool { !sending && !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    private func projectMenu(_ p: Project) -> some View {
        Menu {
            ForEach(projects) { q in
                Button { projectPath = q.path } label: {
                    if q.path == p.path { Label(q.name, systemImage: "checkmark") } else { Text(q.name) }
                }
            }
        } label: {
            chip { Image(systemName: "folder"); Text(p.name) }
        }
        .menuStyle(.button).buttonStyle(.plain).menuIndicator(.hidden).fixedSize()
    }

    private var kindPicker: some View {
        HStack(spacing: 2) {
            ForEach(Self.kinds, id: \.id) { k in
                Button { kind = k.id } label: {
                    Text(k.name).font(.system(size: 14.5, weight: kind == k.id ? .semibold : .regular))
                        .foregroundStyle(kind == k.id ? Color.madFg : Color.madFg3)
                        .padding(.horizontal, 11).frame(height: 30)
                        .background(kind == k.id ? Color.madPaper : Color.clear, in: Capsule())
                        .shadow(color: .black.opacity(kind == k.id ? 0.08 : 0), radius: 1.5, y: 1)
                        .contentShape(Capsule())
                }
                .buttonStyle(.plain)
            }
        }
        .padding(3)
        .background(Color.madRaise, in: Capsule())
    }

    private var modelMenu: some View {
        Menu {
            Button { model = "" } label: {
                if model.isEmpty { Label(L("默认模型"), systemImage: "checkmark") } else { Text(L("默认模型")) }
            }
            if !models.isEmpty { Divider() }
            ForEach(models) { m in
                Button { model = m.value } label: {
                    let name = m.name.isEmpty ? m.value : m.name
                    if model == m.value { Label(name, systemImage: "checkmark") } else { Text(name) }
                }
            }
        } label: {
            chip { Text(modelName) }
        }
        .menuStyle(.button).buttonStyle(.plain).menuIndicator(.hidden).fixedSize()
    }

    private var modelName: String {
        if model.isEmpty { return L("默认模型") }
        return models.first { $0.value == model }.map { $0.name.isEmpty ? $0.value : $0.name } ?? model
    }

    private func chip<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        HStack(spacing: 5) {
            content()
            Image(systemName: "chevron.down").font(.system(size: 9, weight: .semibold)).foregroundStyle(Color.madFg3)
        }
        .font(.system(size: 14.5))
        .foregroundStyle(Color.madFg)
        .padding(.horizontal, 10)
        .frame(height: 34)
        .overlay(Capsule().strokeBorder(Color.madLine, lineWidth: 1))
        .contentShape(Capsule())
    }

    // MARK: earlier sessions

    private func earlier(_ p: Project) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Label(L("接着以前的对话"), systemImage: "clock")
                .font(.system(size: 14, weight: .semibold)).foregroundStyle(Color.madFg2)
                .padding(.horizontal, 10).padding(.bottom, 6)
            ForEach(past.prefix(12)) { s in
                Button { resume(s, in: p) } label: {
                    HStack(spacing: 12) {
                        CharacterView(id: s.id, kind: s.kind, state: .idle, size: 26)
                        Text(s.title.isEmpty ? s.id : s.title).font(.system(size: 16)).foregroundStyle(Color.madFg)
                            .lineLimit(1).truncationMode(.tail)
                        Spacer(minLength: 12)
                        if let u = s.updated {
                            Text(MadWords.ago(Date().timeIntervalSince(u))).font(.system(size: 13.5)).foregroundStyle(Color.madFg3)
                        }
                    }
                    .padding(.horizontal, 10).frame(height: 40)
                    .contentShape(Rectangle())
                }
                .buttonStyle(PastRowStyle())
                .disabled(sending)
            }
        }
    }

    // MARK: actions

    private func send(_ p: Project) {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard canSend, !t.isEmpty else { return }
        start(p, text: t, session: "", cwd: "", kind: kind) { text = "" }
    }

    private func resume(_ s: PastSession, in p: Project) {
        start(p, text: "", session: s.id, cwd: s.cwd, kind: s.kind.isEmpty ? kind : s.kind) {}
    }

    private func start(_ p: Project, text t: String, session: String, cwd: String, kind k: String, then done: @escaping () -> Void) {
        sending = true
        error = nil
        Task {
            defer { sending = false }
            do {
                let r = try await api.startAgent(project: p.path, kind: k, model: session.isEmpty ? model : "", text: t,
                                                 session: session, cwd: cwd)
                // a first message that did not go stays written, and goes with the agent to its page
                if r.failure == nil { done() }
                await started(StartedChat(id: r.id, text: t, failure: r.failure))
            } catch {
                self.error = (error as? LocalizedError)?.errorDescription ?? error.localizedDescription
            }
        }
    }

    private func loadModels() async {
        guard let p = project else { return }
        let list = (try? await api.models(kind: kind, project: p.path)) ?? []
        models = list
        if !model.isEmpty, !list.contains(where: { $0.value == model }) { model = "" }
    }

    private func loadHistory() async {
        guard let p = project else { return }
        past = (try? await api.history(project: p.path)) ?? []
    }
}

private struct PastRowStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(configuration.isPressed ? Color.madRaise : Color.clear, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
    }
}
