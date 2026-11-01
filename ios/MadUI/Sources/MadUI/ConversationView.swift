import SwiftUI

/// An agent's conversation: the header, the messages, what waits on the person pinned above a one-line composer.
/// The same view serves the phone (with a back button) and the Mac's detail pane.
public struct ConversationView: View {
    public var model: AgentConversation
    public var actions: ConversationActions
    /// The phone shows a back button; the Mac's detail pane does not.
    public var showsBack: Bool
    @State private var draft = ""
    @ScaledMetric(relativeTo: .body) private var avatar: CGFloat = 26
    /// The message column's width: a bubble takes a share of it.
    @State private var columnWidth: CGFloat = 0
    #if os(macOS)
    @State private var keeper = BottomKeeper()
    #else
    /// The end marker is on screen: new things are followed only then.
    @State private var atEnd = true
    #endif

    public init(model: AgentConversation, actions: ConversationActions = .init(), showsBack: Bool = true) {
        self.model = model
        self.actions = actions
        self.showsBack = showsBack
    }

    /// What waits on the person, then the composer.
    private var bottomBar: some View {
        VStack(spacing: 0) {
            if let ask = model.ask {
                // from the bubbles' left edge, as wide as its content up to what a bubble may be
                HStack(spacing: 0) {
                    Color.clear.frame(width: MadColumn.gutter, height: 0)
                    AskCardView(card: ask, act: { actions.act(ask.id, $0, $1) })
                        .id(ask.id) // a replacement card starts fresh: what was typed for the old one is not its answer
                        .hugging(maxWidth: columnWidth > 0 ? MadColumn.bubble(in: columnWidth) : 480)
                    Spacer(minLength: 0)
                }
                .frame(maxWidth: MadColumn.maxWidth)
                .padding(.horizontal, MadColumn.side)
                .frame(maxWidth: .infinity)
                .padding(.bottom, 6)
            }
            composer
        }
    }

    public var body: some View {
        #if os(iOS)
        // Full-bleed: the list keeps its frame when the keyboard comes and goes; the head and the bottom bar are insets of
        // it with a material behind them, so what scrolls under them is blurred, never cut.
        messages
            .safeAreaInset(edge: .top, spacing: 0) {
                ConversationHeader(model: model, showsBack: showsBack, back: actions.back, openWeb: actions.openWeb).background(.bar)
            }
            .safeAreaInset(edge: .bottom, spacing: 0) { bottomBar.background(.bar) }
            .background(.background)
            .environment(\.convImageLoader, actions.loadImage)
        #else
        VStack(spacing: 0) {
            messages
            bottomBar
        }
        .background(Color.chatBackdrop)
        .environment(\.convImageLoader, actions.loadImage)
        // no bar: the agent's name floats at the top as a small pill, with the way to its terminal and changes beside it.
        // It hugs its text but never passes the pane (less the side margins): a long title is cut at the end.
        .overlay(alignment: .top) {
            HStack(spacing: 10) {
                ConversationIdentity(model: model, expands: false)
                if let openWeb = actions.openWeb {
                    Button(action: openWeb) {
                        Image(systemName: "terminal").font(.system(size: 14)).foregroundStyle(.secondary)
                            .frame(width: 28, height: 28)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("在网页里打开终端/改动")
                }
            }
            .padding(.horizontal, 14).padding(.vertical, 6)
            .background(.regularMaterial, in: Capsule())
            .overlay(Capsule().strokeBorder(Color.chatLine.opacity(0.6)))
            .shadow(color: .black.opacity(0.08), radius: 8, y: 2)
            .padding(.horizontal, MadColumn.side)
            .padding(.top, 12)
        }
        #endif
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
                        AgentRow(model: model, avatar: avatar, hugs: true) { LiveBubbleView(live: live) }
                            .id("live")
                    }
                    Color.clear.frame(height: 1).id("end")
                        #if os(macOS)
                        .onDisappear { if keeper.leftBottom() { DispatchQueue.main.async { proxy.scrollTo("end") } } }
                        #else
                        .onAppear { atEnd = true }
                        .onDisappear { atEnd = false }
                        #endif
                }
                .frame(maxWidth: MadColumn.maxWidth)
                .background(GeometryReader { g in
                    Color.clear.onAppear { columnWidth = g.size.width }.onChange(of: g.size.width) { _, w in columnWidth = w }
                })
                .environment(\.madColumnWidth, columnWidth)
                .padding(.horizontal, MadColumn.side)
                .frame(maxWidth: .infinity)
                #if os(macOS)
                .padding(.top, 76).padding(.bottom, 8) // under the floating name
                #else
                .padding(.vertical, 8)
                #endif
            }
            .defaultScrollAnchor(.bottom)
            .scrollDismissesKeyboardIfAvailable()
            .onChange(of: model.entries.count) { _, _ in follow(proxy) }
            .onChange(of: model.live) { _, _ in follow(proxy) }
            .onChange(of: model.ask) { _, _ in follow(proxy) }
            #if os(macOS)
            .background(GeometryReader { g in
                Color.clear.onChange(of: g.size) { _, _ in
                    if keeper.resized() { DispatchQueue.main.async { proxy.scrollTo("end") } }
                }
            })
            #endif
            #if os(macOS)
            // the list fades out under the name, which floats over it
            .overlay(alignment: .top) {
                LinearGradient(colors: [Color.chatBackdrop, Color.chatBackdrop.opacity(0)], startPoint: .top, endPoint: .bottom)
                    .frame(height: 72).allowsHitTesting(false)
            }
            #endif
        }
    }

    /// A new entry, the message being written or a question is followed when the person is at the end of the list
    /// (on the Mac: always, as before).
    private func follow(_ proxy: ScrollViewProxy) {
        #if os(iOS)
        guard atEnd else { return }
        #endif
        proxy.scrollTo("end")
    }

    // MARK: composer

    @ViewBuilder private var composer: some View {
        if model.canSend {
            conversationField
            // the message column's edges, exactly
            .frame(maxWidth: MadColumn.maxWidth)
            .padding(.horizontal, MadColumn.side)
            .frame(maxWidth: .infinity)
            .padding(.top, 6)
            #if os(macOS)
            .padding(.bottom, 16)
            #else
            .padding(.bottom, 8)
            #endif
        } else {
            Text(model.sendHint)
                .font(MadType.rowLine)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity)
                .padding(.horizontal, 16)
                .padding(.vertical, 12)
        }
    }

    /// ＋, the field and send: on the Mac one rounded bar with a hairline and a soft shadow, on the phone three pieces.
    @ViewBuilder private var conversationField: some View {
        #if os(macOS)
        HStack(alignment: .bottom, spacing: 8) {
            attachButton
            composerField.padding(.vertical, 7)
            trailing
        }
        .padding(.horizontal, 8).padding(.vertical, 6)
        .background(RoundedRectangle(cornerRadius: 24, style: .continuous).fill(Color.chatComposer))
        .overlay(RoundedRectangle(cornerRadius: 24, style: .continuous).strokeBorder(Color.chatLine))
        .shadow(color: .black.opacity(0.07), radius: 10, y: 3)
        #else
        HStack(alignment: .bottom, spacing: 8) {
            attachButton
            composerField
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
                .background(RoundedRectangle(cornerRadius: 19, style: .continuous).fill(Color.primary.opacity(0.08)))
            trailing
        }
        #endif
    }

    private var attachButton: some View {
        Button(action: actions.attach) {
            Image(systemName: "plus")
                .font(.system(size: 17, weight: .medium))
                .foregroundStyle(.secondary)
                .frame(width: 36, height: 36)
                #if os(iOS)
                .background(Circle().fill(Color.primary.opacity(0.08)))
                #endif
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Attach")
    }

    private var composerField: some View {
        TextField(model.composerPrompt, text: $draft, axis: .vertical)
            .lineLimit(1...5)
            .textFieldStyle(.plain)
            .font(MadType.message)
            .onSubmit(submit)
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
    var openWeb: (() -> Void)?

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
            ConversationIdentity(model: model)
            if let openWeb {
                Button(action: openWeb) {
                    Image(systemName: "terminal")
                        .font(.system(size: 17))
                        .foregroundStyle(.secondary)
                        .frame(width: 36, height: 36)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("在网页里打开终端/改动")
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
    }
}

/// The character, the name and the state line, read as one. On the Mac it is the window's toolbar.
struct ConversationIdentity: View {
    let model: AgentConversation
    /// Fills the width it is given (a header's row); the Mac's pill hugs its text instead.
    var expands = true

    var body: some View {
        HStack(spacing: 10) {
            CharacterView(id: model.agentID, kind: model.kind, state: model.state, size: 34, dot: model.needsYou)
                .frame(width: 34, height: 34)
            VStack(alignment: .leading, spacing: 0) {
                Text(model.title)
                    .font(MadType.title)
                    .lineLimit(1)
                PillSubline(text: model.stateLine)
            }
            if expands { Spacer(minLength: 0) }
        }
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
    /// A bubble is as wide as its text, up to its share of the column; a line (a tool, a picture) has the column.
    var hugs = false
    @ViewBuilder var content: Content
    @Environment(\.madColumnWidth) private var columnWidth

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            #if os(iOS)
            CharacterView(id: model.agentID, kind: model.kind, state: .idle, size: avatar)
                .frame(width: avatar, height: avatar)
                .accessibilityHidden(true)
            #endif
            Group {
                if hugs && columnWidth > 0 { content.hugging(maxWidth: MadColumn.bubble(in: columnWidth)) } else { content }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

struct EntryView: View {
    let entry: ConvEntry
    let model: AgentConversation
    let avatar: CGFloat
    let loadEarlier: (() -> Void)?
    @Environment(\.madColumnWidth) private var columnWidth

    var body: some View {
        Group {
            switch entry.content {
            case let .user(text, images):
                HStack(spacing: 0) {
                    Spacer(minLength: MadColumn.gutter)
                    VStack(alignment: .trailing, spacing: 4) {
                        ForEach(Array(images.enumerated()), id: \.offset) { ImageView(image: $0.element) }
                        if !text.isEmpty {
                            Text(text)
                                .font(MadType.message)
                                .lineSpacing(MadType.messageLineSpacing)
                                .fixedSize(horizontal: false, vertical: true)
                                .foregroundStyle(.white)
                                .padding(.horizontal, 13)
                                .padding(.vertical, 8)
                                .hugging(maxWidth: columnWidth > 0 ? MadColumn.bubble(in: columnWidth) : 480)
                                .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(ConversationStyle.blue))
                        }
                    }
                }
            case let .text(markdown):
                AgentRow(model: model, avatar: avatar, hugs: true) { MarkdownBubble(markdown: markdown) }
            case let .tool(tool):
                AgentRow(model: model, avatar: avatar, hugs: true) { ToolRow(tool: tool) }
            case let .image(image):
                AgentRow(model: model, avatar: avatar) { ImageView(image: image) }
            case let .earlier(note):
                let label = Text(note)
                    .font(MadType.label)
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
                case let .text(s): Text(Self.attributed(s)).font(MadType.message).lineSpacing(MadType.messageLineSpacing)
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
                .font(MadType.code)
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
        HStack(spacing: 10) {
            if tool.running {
                ProgressView().controlSize(.mini)
            } else if tool.error {
                Image(systemName: "exclamationmark.circle.fill").foregroundStyle(ConversationStyle.red)
            }
            summary
                .font(MadType.pill)
                .lineLimit(open ? nil : 1)
                .truncationMode(.tail)
            if tool.hasDetail {
                Image(systemName: "chevron.right")
                    .font(MadType.label.weight(.semibold))
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
                .font(MadType.codeSmall)
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
                    if !image.alt.isEmpty { Text(image.alt).font(MadType.label).lineLimit(1).padding(.horizontal, 6) }
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
        .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(.background))
        .overlay(RoundedRectangle(cornerRadius: 18, style: .continuous).strokeBorder(ConversationStyle.red.opacity(0.45), lineWidth: 1.5))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(card.accessibilityLabel)
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(card.title).font(MadType.pill.weight(.bold))
            if !card.detail.isEmpty {
                if card.mono {
                    ScrollView(.horizontal, showsIndicators: false) {
                        Text(card.detail)
                            .font(MadType.code)
                            .fixedSize()
                            .padding(.horizontal, 10)
                            .padding(.vertical, 8)
                    }
                    .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(ConversationStyle.bubble))
                } else {
                    Text(card.detail).font(MadType.pill).foregroundStyle(.secondary)
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
                .font(MadType.pill.weight(primary ? .semibold : .regular))
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
