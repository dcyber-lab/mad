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
                AskCardView(card: ask, act: { actions.act(ask.id, $0, $1) })
                    .id(ask.id) // a replacement card starts fresh: what was typed for the old one is not its answer
                    .padding(.horizontal, 12)
                    .padding(.bottom, 6)
            }
            composer
        }
        .background(.background)
        .environment(\.convImageLoader, actions.loadImage)
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
            // the character, the name and the state line read as one; Back stays a button of its own
            HStack(spacing: 10) {
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
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("\(model.title), \(model.stateLine)")
            .accessibilityAddTraits(.isHeader)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
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
    let loadEarlier: (() -> Void)?

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
                let label = Text(note)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 6)
                if let loadEarlier {
                    Button(action: loadEarlier) { label }.buttonStyle(.plain)
                } else {
                    label
                }
            }
        }
        .modifier(EntryAccessibility(entry: entry, agent: model.title))
    }
}

/// An entry reads as one element, except a tool row: it keeps its own elements (the toggle, and the input and result once open).
private struct EntryAccessibility: ViewModifier {
    let entry: ConvEntry
    let agent: String

    func body(content: Content) -> some View {
        if case .tool = entry.content {
            content
        } else {
            content
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(entry.accessibilityLabel(agent: agent))
        }
    }
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
/// The toggle is one element (the summary, expanded or collapsed); the input and result are readable elements of their own.
struct ToolRow: View {
    let tool: ConvTool
    @State private var open = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if tool.hasDetail {
                Button { withAnimation(.snappy(duration: 0.2)) { open.toggle() } } label: { line }
                    .buttonStyle(.plain)
                    .accessibilityLabel(tool.accessibilityLabel)
                    .accessibilityValue(open ? "expanded" : "collapsed")
                    .accessibilityHint(open ? "collapses" : "expands")
            } else {
                line
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(tool.accessibilityLabel)
            }
            if open {
                if !tool.input.isEmpty { DetailBox(title: "input", text: tool.input, error: false) }
                if let result = tool.result, !result.isEmpty {
                    DetailBox(title: tool.error ? "error" : "result", text: result, error: tool.error)
                }
            }
        }
        .padding(.horizontal, 11)
        .padding(.vertical, 7)
        .background(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Color.primary.opacity(0.14)))
        .accessibilityElement(children: .contain)
    }

    private var line: some View {
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

    private var summary: Text {
        tool.label.isEmpty
            ? Text(tool.name).bold()
            : Text(tool.name).bold() + Text(" · \(tool.label)").foregroundColor(.secondary)
    }
}

private struct DetailBox: View {
    let title: String
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
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(title), \(text)")
    }
}

struct ConvImageLoaderKey: EnvironmentKey {
    static let defaultValue: ((ConvImage) async -> Data?)? = nil
}

extension EnvironmentValues {
    var convImageLoader: ((ConvImage) async -> Data?)? {
        get { self[ConvImageLoaderKey.self] }
        set { self[ConvImageLoaderKey.self] = newValue }
    }
}

struct ImageView: View {
    let image: ConvImage
    @Environment(\.convImageLoader) private var loader
    @State private var loaded: Image?

    var body: some View {
        Group {
            if let loaded {
                loaded.resizable().scaledToFit()
            } else if let url = image.url {
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
        .task(id: image) {
            guard !image.source.isEmpty, let loader, let data = await loader(image) else { return }
            loaded = Self.decode(data)
        }
    }

    static func decode(_ data: Data) -> Image? {
        #if os(iOS)
        UIImage(data: data).map(Image.init(uiImage:))
        #else
        NSImage(data: data).map(Image.init(nsImage:))
        #endif
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
    @State private var contentHeight: CGFloat = 0
    static let maxHeight: CGFloat = 320

    var body: some View {
        // at the largest text sizes the card would fill the screen: past a height it scrolls inside itself
        ScrollView {
            content.background(GeometryReader { Color.clear.preference(key: AskHeightKey.self, value: $0.size.height) })
        }
        .scrollBounceBehavior(.basedOnSize)
        .frame(height: contentHeight > 0 ? min(contentHeight, Self.maxHeight) : nil)
        .onPreferenceChange(AskHeightKey.self) { contentHeight = $0 }
        .padding(.horizontal, 15)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(.background))
        .overlay(RoundedRectangle(cornerRadius: 18, style: .continuous).strokeBorder(ConversationStyle.red.opacity(0.45), lineWidth: 1.5))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(card.accessibilityLabel)
    }

    private var content: some View {
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
                TextField(asking.label, text: $text, axis: .vertical)
                    .lineLimit(1...4)
                    .textFieldStyle(.plain)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 7)
                    .background(RoundedRectangle(cornerRadius: 16, style: .continuous).fill(ConversationStyle.bubble))
                FlowLayout(spacing: 8) {
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
    }

    private func chip(_ label: String, primary: Bool, _ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(label)
                .font(.subheadline.weight(primary ? .semibold : .regular))
                .foregroundStyle(primary ? Color.white : Color.primary)
                .multilineTextAlignment(.leading)
                .fixedSize(horizontal: false, vertical: true) // a long label wraps inside its button
                .padding(.horizontal, 16)
                .padding(.vertical, 8)
                .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(primary ? ConversationStyle.blue : ConversationStyle.bubble))
        }
        .buttonStyle(.plain)
    }
}

private struct AskHeightKey: PreferenceKey {
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
}

/// Lays children left to right and wraps to the next line, so the acts never run past the screen.
/// Each child is asked for at most the container's width: one too wide for the rest of a line goes on a line of its own,
/// and one wider than the container wraps its own text inside.
struct FlowLayout: Layout {
    var spacing: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? .infinity
        return Self.arrange(sizes: measure(subviews, width: width), width: width, spacing: spacing).size
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let sizes = measure(subviews, width: bounds.width)
        let laid = Self.arrange(sizes: sizes, width: bounds.width, spacing: spacing)
        for (i, view) in subviews.enumerated() {
            view.place(at: CGPoint(x: bounds.minX + laid.origins[i].x, y: bounds.minY + laid.origins[i].y),
                       proposal: ProposedViewSize(width: sizes[i].width, height: sizes[i].height))
        }
    }

    private func measure(_ subviews: Subviews, width: CGFloat) -> [CGSize] {
        subviews.map { view in
            let s = view.sizeThatFits(ProposedViewSize(width: width.isFinite ? width : nil, height: nil))
            return CGSize(width: width.isFinite ? min(s.width, width) : s.width, height: s.height)
        }
    }

    /// Where each size goes: left to right, a new line when the next does not fit, no size wider than `width`.
    static func arrange(sizes: [CGSize], width: CGFloat, spacing: CGFloat) -> (size: CGSize, origins: [CGPoint]) {
        var x: CGFloat = 0, y: CGFloat = 0, rowHeight: CGFloat = 0, maxX: CGFloat = 0
        var origins: [CGPoint] = []
        for s in sizes {
            if x > 0, x + s.width > width { x = 0; y += rowHeight + spacing; rowHeight = 0 }
            origins.append(CGPoint(x: x, y: y))
            x += s.width + spacing
            rowHeight = max(rowHeight, s.height)
            maxX = max(maxX, x - spacing)
        }
        return (CGSize(width: maxX, height: y + rowHeight), origins)
    }
}
