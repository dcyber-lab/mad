import SwiftUI

/// An agent's conversation: the header, the messages, what waits on the person pinned above a one-line composer.
/// The same view serves the phone (with a back button) and the Mac's detail pane.
public struct ConversationView: View {
    public var model: AgentConversation
    public var actions: ConversationActions
    /// The phone shows a back button; the Mac's detail pane does not.
    public var showsBack: Bool
    @State private var draft = ""
    @ScaledMetric(relativeTo: .body) private var avatar: CGFloat = 28

    public init(model: AgentConversation, actions: ConversationActions = .init(), showsBack: Bool = true) {
        self.model = model
        self.actions = actions
        self.showsBack = showsBack
    }

    public var body: some View {
        VStack(spacing: 0) {
            ConversationHeader(model: model, showsBack: showsBack, back: actions.back)
            messages
            if let ask = model.ask {
                AskCardView(card: ask, act: actions.act)
                    .padding(.horizontal, 12)
                    .padding(.bottom, 6)
            }
            composer
        }
        .background(.background)
    }

    // MARK: messages

    private var messages: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 8) {
                    ForEach(model.entries) { entry in
                        EntryView(entry: entry, model: model, avatar: avatar, loadEarlier: actions.loadEarlier)
                            .id(entry.id)
                    }
                    if let live = model.live {
                        AgentRow(model: model, avatar: avatar) { LiveBubbleView(live: live) }
                            .id("live")
                    }
                    Color.clear.frame(height: 1).id("end")
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
            }
            .defaultScrollAnchor(.bottom)
            .scrollDismissesKeyboardIfAvailable()
            .onChange(of: model.entries.count) { _, _ in proxy.scrollTo("end") }
            .onChange(of: model.live) { _, _ in proxy.scrollTo("end") }
            .onChange(of: model.ask) { _, _ in proxy.scrollTo("end") }
        }
    }

    // MARK: composer

    @ViewBuilder private var composer: some View {
        if model.canSend {
            HStack(alignment: .bottom, spacing: 8) {
                Button(action: actions.attach) {
                    Image(systemName: "plus")
                        .font(.system(size: 17, weight: .medium))
                        .foregroundStyle(.secondary)
                        .frame(width: 36, height: 36)
                        .background(Circle().fill(Color.primary.opacity(0.08)))
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Attach")
                TextField(model.composerPrompt, text: $draft, axis: .vertical)
                    .lineLimit(1...5)
                    .textFieldStyle(.plain)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 8)
                    .background(RoundedRectangle(cornerRadius: 19, style: .continuous).fill(Color.primary.opacity(0.08)))
                    .onSubmit(submit)
                trailing
            }
            .padding(.horizontal, 12)
            .padding(.top, 6)
            .padding(.bottom, 8)
        } else {
            Text(model.sendHint)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity)
                .padding(.horizontal, 16)
                .padding(.vertical, 12)
        }
    }

    @ViewBuilder private var trailing: some View {
        if model.showsInterrupt(draft: draft) {
            Button(action: actions.interrupt) {
                Image(systemName: "stop.fill")
                    .font(.system(size: 14, weight: .bold))
                    .foregroundStyle(.white)
                    .frame(width: 36, height: 36)
                    .background(Circle().fill(Color(hex: 0xff3b30)))
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Interrupt")
        } else {
            Button(action: submit) {
                Image(systemName: "arrow.up")
                    .font(.system(size: 17, weight: .bold))
                    .foregroundStyle(.white)
                    .frame(width: 36, height: 36)
                    .background(Circle().fill(ConversationStyle.blue.opacity(model.canSubmit(draft: draft) ? 1 : 0.35)))
            }
            .buttonStyle(.plain)
            .disabled(!model.canSubmit(draft: draft))
            .accessibilityLabel("Send")
        }
    }

    private func submit() {
        guard model.canSubmit(draft: draft) else { return }
        actions.send(draft.trimmingCharacters(in: .whitespacesAndNewlines))
        draft = ""
    }
}

enum ConversationStyle {
    static let blue = Color(hex: 0x1f6bff)
    static let red = Color(hex: 0xff3b30)
    static let bubble = Color.primary.opacity(0.08)
}

private extension View {
    func scrollDismissesKeyboardIfAvailable() -> some View {
        #if os(iOS)
        scrollDismissesKeyboard(.interactively)
        #else
        self
        #endif
    }
}

// MARK: header

struct ConversationHeader: View {
    let model: AgentConversation
    let showsBack: Bool
    let back: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            if showsBack {
                Button(action: back) {
                    Image(systemName: "chevron.left")
                        .font(.system(size: 19, weight: .semibold))
                        .foregroundStyle(.secondary)
                        .frame(width: 30, height: 36)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Back")
            }
            CharacterView(id: model.agentID, kind: model.kind, state: model.state, size: 34, dot: model.needsYou)
                .frame(width: 34, height: 34)
            VStack(alignment: .leading, spacing: 0) {
                Text(model.title)
                    .font(.headline)
                    .lineLimit(1)
                Text(model.stateLine)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(model.title), \(model.stateLine)")
        .accessibilityAddTraits(.isHeader)
    }
}

// MARK: entries

/// An agent's side of the thread: its small character, then what it said, never wider than the column.
struct AgentRow<Content: View>: View {
    let model: AgentConversation
    let avatar: CGFloat
    @ViewBuilder var content: Content

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            CharacterView(id: model.agentID, kind: model.kind, state: .idle, size: avatar)
                .frame(width: avatar, height: avatar)
                .accessibilityHidden(true)
            content
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

struct EntryView: View {
    let entry: ConvEntry
    let model: AgentConversation
    let avatar: CGFloat
    let loadEarlier: () -> Void

    var body: some View {
        Group {
            switch entry.content {
            case let .user(text, images):
                HStack(spacing: 0) {
                    Spacer(minLength: 48)
                    VStack(alignment: .trailing, spacing: 4) {
                        ForEach(Array(images.enumerated()), id: \.offset) { ImageView(image: $0.element) }
                        if !text.isEmpty {
                            Text(text)
                                .foregroundStyle(.white)
                                .padding(.horizontal, 13)
                                .padding(.vertical, 8)
                                .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(ConversationStyle.blue))
                        }
                    }
                }
            case let .text(markdown):
                AgentRow(model: model, avatar: avatar) { MarkdownBubble(markdown: markdown) }
            case let .tool(tool):
                AgentRow(model: model, avatar: avatar) { ToolRow(tool: tool) }
            case let .image(image):
                AgentRow(model: model, avatar: avatar) { ImageView(image: image) }
            case let .earlier(note):
                Button(action: loadEarlier) {
                    Text(note)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 6)
                }
                .buttonStyle(.plain)
            }
        }
        .accessibilityElement(children: entry.isTool ? .contain : .ignore)
        .accessibilityLabel(entry.isTool ? "" : entry.accessibilityLabel(agent: model.title))
    }
}

private extension ConvEntry {
    /// A tool row is a button with its own label; the others read as one element.
    var isTool: Bool { if case .tool = content { true } else { false } }
}

struct MarkdownBubble: View {
    let markdown: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(Markdown.blocks(markdown).enumerated()), id: \.offset) { _, block in
                switch block {
                case let .text(s): Text(Self.attributed(s))
                case let .code(s): CodeBlock(code: s)
                }
            }
        }
        .textSelection(.enabled)
        .padding(.horizontal, 13)
        .padding(.vertical, 8)
        .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(ConversationStyle.bubble))
    }

    static func attributed(_ s: String) -> AttributedString {
        (try? AttributedString(markdown: s, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(s)
    }
}

/// Code scrolls sideways inside its own box; it never widens the bubble.
struct CodeBlock: View {
    let code: String

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Text(code)
                .font(.system(.footnote, design: .monospaced))
                .fixedSize()
                .padding(10)
        }
        .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(Color.primary.opacity(0.07)))
    }
}

struct LiveBubbleView: View {
    let live: LiveBubble

    var body: some View {
        Group {
            if live.markdown.isEmpty {
                ProgressView().controlSize(.small).padding(.horizontal, 13).padding(.vertical, 10)
                    .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(ConversationStyle.bubble))
            } else {
                MarkdownBubble(markdown: live.markdown + " ▍")
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(live.markdown.isEmpty ? "writing" : "writing, \(Markdown.plain(live.markdown))")
    }
}

/// "Tool · label" on one line; open, it shows the input and the result (red when it failed).
struct ToolRow: View {
    let tool: ConvTool
    @State private var open = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Button { if tool.hasDetail { withAnimation(.snappy(duration: 0.2)) { open.toggle() } } } label: {
                HStack(spacing: 8) {
                    if tool.running {
                        ProgressView().controlSize(.mini)
                    } else if tool.error {
                        Image(systemName: "exclamationmark.circle.fill").foregroundStyle(ConversationStyle.red)
                    }
                    summary
                        .font(.subheadline)
                        .lineLimit(open ? nil : 1)
                        .truncationMode(.tail)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    if tool.hasDetail {
                        Image(systemName: "chevron.right")
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.secondary)
                            .rotationEffect(.degrees(open ? 90 : 0))
                    }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if open {
                if !tool.input.isEmpty { DetailBox(text: tool.input, error: false) }
                if let result = tool.result, !result.isEmpty { DetailBox(text: result, error: tool.error) }
            }
        }
        .padding(.horizontal, 11)
        .padding(.vertical, 7)
        .background(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Color.primary.opacity(0.14)))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(tool.accessibilityLabel)
        .accessibilityHint(tool.hasDetail ? (open ? "collapses" : "expands") : "")
        .accessibilityAddTraits(tool.hasDetail ? .isButton : [])
        .accessibilityAction { if tool.hasDetail { open.toggle() } }
    }

    private var summary: Text {
        tool.label.isEmpty
            ? Text(tool.name).bold()
            : Text(tool.name).bold() + Text(" · \(tool.label)").foregroundColor(.secondary)
    }
}

private struct DetailBox: View {
    let text: String
    let error: Bool

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Text(text)
                .font(.system(.caption, design: .monospaced))
                .foregroundStyle(error ? ConversationStyle.red : .primary)
                .fixedSize()
                .padding(8)
        }
        .frame(maxHeight: 220)
        .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Color.primary.opacity(0.06)))
    }
}

struct ImageView: View {
    let image: ConvImage

    var body: some View {
        Group {
            if let url = image.url {
                AsyncImage(url: url) { phase in
                    switch phase {
                    case let .success(img): img.resizable().scaledToFit()
                    default: placeholder
                    }
                }
            } else {
                placeholder
            }
        }
        .frame(maxWidth: 240, maxHeight: 240)
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    private var placeholder: some View {
        RoundedRectangle(cornerRadius: 14, style: .continuous)
            .fill(Color.primary.opacity(0.08))
            .frame(width: 160, height: 110)
            .overlay {
                VStack(spacing: 4) {
                    Image(systemName: "photo").font(.title3)
                    if !image.alt.isEmpty { Text(image.alt).font(.caption).lineLimit(1).padding(.horizontal, 6) }
                }
                .foregroundStyle(.secondary)
            }
    }
}

// MARK: ask

/// What waits on the person, pinned above the composer with a red outline. An act that takes words asks for them first.
struct AskCardView: View {
    let card: AskCard
    let act: (String, String?) -> Void
    @State private var asking: AskAct?
    @State private var text = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(card.title).font(.subheadline.weight(.bold))
            if !card.detail.isEmpty {
                if card.mono {
                    ScrollView(.horizontal, showsIndicators: false) {
                        Text(card.detail)
                            .font(.system(.footnote, design: .monospaced))
                            .fixedSize()
                            .padding(.horizontal, 10)
                            .padding(.vertical, 8)
                    }
                    .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(ConversationStyle.bubble))
                } else {
                    Text(card.detail).font(.subheadline).foregroundStyle(.secondary)
                }
            }
            if let asking {
                HStack(spacing: 8) {
                    TextField(asking.label, text: $text, axis: .vertical)
                        .lineLimit(1...4)
                        .textFieldStyle(.plain)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 7)
                        .background(RoundedRectangle(cornerRadius: 16, style: .continuous).fill(ConversationStyle.bubble))
                    chip("取消", primary: false) { self.asking = nil; text = "" }
                    chip(asking.label, primary: true) {
                        act(asking.id, text.trimmingCharacters(in: .whitespacesAndNewlines))
                        self.asking = nil
                        text = ""
                    }
                }
            } else {
                FlowLayout(spacing: 8) {
                    ForEach(card.acts) { a in
                        chip(a.label, primary: a.primary) {
                            if a.input { asking = a } else { act(a.id, nil) }
                        }
                    }
                }
            }
        }
        .padding(.horizontal, 15)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(.background))
        .overlay(RoundedRectangle(cornerRadius: 18, style: .continuous).strokeBorder(ConversationStyle.red.opacity(0.45), lineWidth: 1.5))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(card.accessibilityLabel)
    }

    private func chip(_ label: String, primary: Bool, _ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(label)
                .font(.subheadline.weight(primary ? .semibold : .regular))
                .foregroundStyle(primary ? Color.white : Color.primary)
                .lineLimit(1)
                .padding(.horizontal, 16)
                .padding(.vertical, 8)
                .background(Capsule().fill(primary ? ConversationStyle.blue : ConversationStyle.bubble))
        }
        .buttonStyle(.plain)
    }
}

/// Lays children left to right and wraps to the next line, so the acts never run past the screen.
struct FlowLayout: Layout {
    var spacing: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        arrange(width: proposal.width ?? .infinity, subviews: subviews).size
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let laid = arrange(width: bounds.width, subviews: subviews)
        for (view, origin) in zip(subviews, laid.origins) {
            view.place(at: CGPoint(x: bounds.minX + origin.x, y: bounds.minY + origin.y), proposal: .unspecified)
        }
    }

    private func arrange(width: CGFloat, subviews: Subviews) -> (size: CGSize, origins: [CGPoint]) {
        var x: CGFloat = 0, y: CGFloat = 0, rowHeight: CGFloat = 0, maxX: CGFloat = 0
        var origins: [CGPoint] = []
        for view in subviews {
            let s = view.sizeThatFits(.unspecified)
            if x > 0, x + s.width > width { x = 0; y += rowHeight + spacing; rowHeight = 0 }
            origins.append(CGPoint(x: x, y: y))
            x += s.width + spacing
            rowHeight = max(rowHeight, s.height)
            maxX = max(maxX, x - spacing)
        }
        return (CGSize(width: maxX, height: y + rowHeight), origins)
    }
}
