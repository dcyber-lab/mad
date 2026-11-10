import MadKit
import MadUI
import SwiftUI

/// 新群聊, in the window's detail pane on the Mac and as a screen over the home list on the iPhone (the page's dialog,
/// natively): a name, what it is for, and the agents to put in it. Creating it opens it.
struct NewChannelDetail: View {
    let api: NewChannelAPI
    let candidates: [(agent: Agent, project: String)]
    /// Opens the channel that was made (after the deck has it).
    let created: (String) async -> Void
    var onBack: (() -> Void)?

    @State private var name = ""
    @State private var topic = ""
    @State private var chosen: Set<String> = []
    @State private var sending = false
    @State private var error: String?

    private var trimmed: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        FormPage(title: L("新群聊"), note: L("群里的 agent 想看的时候会看；@ 某个 agent 会叫醒它。"), onBack: onBack) {
            TextField(L("名字，比如 api-redesign"), text: $name).formText()
                .accessibilityIdentifier("newchannel.name")
                .onSubmit(create)
            TextField(L("用来做什么（可不填）"), text: $topic).formText()
                .onSubmit(create)
            FormField(label: L("成员")) {
                if candidates.isEmpty {
                    Text(L("还没有可以拉进群的 agent。")).font(.system(size: 14)).foregroundStyle(Color.madFg3)
                } else {
                    VStack(spacing: 2) { ForEach(candidates, id: \.agent.id) { row($0.agent, $0.project) } }
                }
            }
            FormError(text: error)
            FormSubmit(title: L("创建"), busy: sending ? L("创建中…") : nil, enabled: !trimmed.isEmpty, action: create)
                .accessibilityIdentifier("newchannel.create")
        }
    }

    private func row(_ a: Agent, _ project: String) -> some View {
        let on = chosen.contains(a.id)
        return Button { if on { chosen.remove(a.id) } else { chosen.insert(a.id) } } label: {
            HStack(spacing: 12) {
                Image(systemName: on ? "checkmark.square.fill" : "square").font(.system(size: 17))
                    .foregroundStyle(on ? Color.madFg : Color.madFg3)
                CharacterView(id: a.id, kind: a.kind, state: .idle, size: 26)
                Text(a.label).font(.system(size: 15)).foregroundStyle(Color.madFg).lineLimit(1).truncationMode(.tail)
                Spacer(minLength: 12)
                Text(project).font(.system(size: 13)).foregroundStyle(Color.madFg3).lineLimit(1)
            }
            .padding(.horizontal, 6).frame(height: 40)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(sending)
    }

    private func create() {
        guard !trimmed.isEmpty, !sending else { return }
        sending = true
        error = nil
        Task {
            defer { sending = false }
            do {
                let c = try await api.createChannel(name: trimmed, topic: topic.trimmingCharacters(in: .whitespacesAndNewlines),
                                                    members: candidates.map(\.agent.id).filter { chosen.contains($0) })
                await created(c.id)
            } catch {
                // what was typed stays, to be fixed
                self.error = (error as? LocalizedError)?.errorDescription ?? error.localizedDescription
            }
        }
    }
}
