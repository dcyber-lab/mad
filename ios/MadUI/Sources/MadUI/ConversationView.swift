import SwiftUI

/// An agent's page, as the approved design (v5) draws it: a head with the avatar, the name and `kind · project / branch
/// · $cost`, two tabs (对话 and 改动) and ⋯ (在终端打开); the conversation, with what waits on the person as a card at
/// its end (on the phone a sheet that rises from the bottom in the composer's place); a one-line composer. On the Mac
/// a Details pane opens at the right (⌘I). The same view serves the phone (with a back button) and the Mac's detail.
public struct ConversationView: View {
    public var model: AgentConversation
    public var actions: ConversationActions
    /// The phone shows a back button; the Mac's detail pane does not.
    public var showsBack: Bool
    @State private var draft = ""
    @State private var tab = ConvTab.chat
    @State private var detailsOpen = false
    @State private var renaming = false
    @State private var newName = ""
    @ScaledMetric(relativeTo: .body) private var avatar: CGFloat = 26
    /// The message column's width: a bubble takes a share of it.
    @State private var columnWidth: CGFloat = 0
    #if os(macOS)
    @State private var keeper = BottomKeeper()
    #else
    /// The end marker is on screen: new things are followed only then.
    @State private var atEnd = true
    @State private var endShown = true
    @State private var endFoot: CGFloat = 0
    @State private var viewportHeight: CGFloat = 0
    @State private var bottomInset: CGFloat = 0
    /// See ChannelView: at its end when last judged with the same viewport and inset.
    @State private var endLatch = true
    @State private var judgedViewport: CGFloat = -1
    @State private var judgedInset: CGFloat = -1
    #endif

    public init(model: AgentConversation, actions: ConversationActions = .init(), showsBack: Bool = true) {
        self.model = model
        self.actions = actions
        self.showsBack = showsBack
    }

    /// The phone's foot: what waits on the person as a sheet in the composer's place, else the composer. (The Mac's
    /// ask card is at the end of the conversation.)
    @ViewBuilder private var bottomBar: some View {
        #if os(iOS)
        if let ask = model.ask {
            AskSheet(card: ask, act: { actions.act(ask.id, $0, $1) })
                .id(ask.id) // a replacement card starts fresh: what was typed for the old one is not its answer
        } else {
            composer
        }
        #else
        composer
        #endif
    }

    public var body: some View {
        #if os(iOS)
        // Full-bleed: the list keeps its frame when the keyboard comes and goes; the head and the bottom bar are insets of
        // it with a material behind them, so what scrolls under them is blurred, never cut.
        Group {
            if tab == .chat {
                messages
                    .safeAreaInset(edge: .bottom, spacing: 0) { bottomBar.background(.ultraThinMaterial) }
            } else {
                ChangesTab(details: model.details, actions: actions)
            }
        }
        .safeAreaInset(edge: .top, spacing: 0) {
            VStack(spacing: 8) {
                ConversationHeader(model: model, showsBack: showsBack, actions: actions)
                ConvTabs(tab: $tab, changes: model.details?.changed ?? 0)
            }
            .padding(.bottom, 8)
            .background(.ultraThinMaterial)
            .overlay(alignment: .bottom) { Rectangle().fill(Color.madLine).frame(height: 1) }
        }
        .background(Color.madPaper)
        .environment(\.madAvatarBackdrop, Color.madPaper)
        .environment(\.convImageLoader, actions.loadImage)
        #else
        VStack(spacing: 0) {
            ConversationHead(model: model, tab: $tab, detailsOpen: $detailsOpen, actions: actions)
            if tab == .chat {
                messages
                bottomBar
            } else {
                ChangesTab(details: model.details, actions: actions)
            }
        }
        .background(Color.madPaper)
        .environment(\.madAvatarBackdrop, Color.madPaper)
        .environment(\.convImageLoader, actions.loadImage)
        .inspector(isPresented: $detailsOpen) {
            AgentDetailsPane(model: model, actions: actions, rename: { newName = model.title; renaming = true })
                .inspectorColumnWidth(min: 240, ideal: 272, max: 340)
        }
        .alert(L("改名"), isPresented: $renaming) {
            TextField(L("名字"), text: $newName)
            Button(L("保存")) {
                let name = newName.trimmingCharacters(in: .whitespacesAndNewlines)
                if !name.isEmpty { actions.rename?(name) }
            }
            Button(L("取消"), role: .cancel) {}
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
                    #if os(macOS)
                    if let ask = model.ask {
                        AskCardView(card: ask, agentID: model.agentID, agentKind: model.kind, act: { actions.act(ask.id, $0, $1) })
                            .id(ask.id) // a replacement card starts fresh: what was typed for the old one is not its answer
                            .padding(.top, 8)
                    }
                    #endif
                    Color.clear.frame(height: 1).id("end")
                        #if os(macOS)
                        .onDisappear { if keeper.leftBottom() { DispatchQueue.main.async { proxy.scrollTo("end") } } }
                        #else
                        .background(GeometryReader { g in
                            Color.clear.onAppear { endFoot = g.frame(in: .named("conversationList")).maxY; endShown = true; judgeEnd() }
                                .onChange(of: g.frame(in: .named("conversationList")).maxY) { _, y in endFoot = y; judgeEnd() }
                        })
                        .onDisappear {
                            endShown = false
                            atEnd = false
                            if judgedViewport == viewportHeight && judgedInset == bottomInset { endLatch = false }
                        }
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
                .padding(.top, 22).padding(.bottom, 8)
                #else
                .padding(.vertical, 8)
                #endif
            }
            .defaultScrollAnchor(.bottom)
            .pinnedToBottomThroughResizes()
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
            #else
            .coordinateSpace(name: "conversationList")
            .background(GeometryReader { g in
                Color.clear.onAppear { viewportHeight = g.size.height; bottomInset = g.safeAreaInsets.bottom; judgeEnd() }
                    .onChange(of: g.size.height) { _, h in
                        viewportHeight = h
                        judgeEnd()
                        if endLatch { proxy.scrollTo("end", anchor: .bottom) }
                    }
                    .onChange(of: g.safeAreaInsets.bottom) { _, inset in
                        // the keyboard, the ask card or the composer changed what covers the foot: a list at its end is put
                        // back at its end once
                        bottomInset = inset
                        judgeEnd()
                        if endLatch { proxy.scrollTo("end", anchor: .bottom) }
                    }
            })
            #endif
        }
    }

    /// A new entry, the message being written or a question is followed when the person is at the end of the list
    /// (on the Mac: always, as before).
    private func follow(_ proxy: ScrollViewProxy) {
        #if os(iOS)
        guard atEnd else { return }
        #endif
        proxy.scrollTo("end", anchor: .bottom)
    }

    #if os(iOS)
    /// At the end: the end marker is realized and its foot is within the list's visible height (its frame is already cut
    /// down by the head, the composer and the keyboard).
    private func judgeEnd() {
        atEnd = endShown && endFoot <= viewportHeight + 2
        if judgedViewport == viewportHeight && judgedInset == bottomInset { endLatch = atEnd }
        judgedViewport = viewportHeight
        judgedInset = bottomInset
    }
    #endif

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
                    .font(.mad(14, weight: .bold))
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
    /// Errors only: a failed tool call.
    static var red: Color { .madDanger }
    static let bubble = Color.primary.opacity(0.08)
}

/// The person's own bubble (the design's `.msg-me`): rounded 16, its bottom right corner 4.
struct MineShape: Shape {
    func path(in rect: CGRect) -> Path {
        UnevenRoundedRectangle(topLeadingRadius: 16, bottomLeadingRadius: 16, bottomTrailingRadius: 4, topTrailingRadius: 16,
                               style: .continuous).path(in: rect)
    }
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

// MARK: head

/// The agent page's two tabs.
enum ConvTab: Hashable { case chat, changes }

/// 对话 | 改动 (with the number of files changed), as a small segmented control.
struct ConvTabs: View {
    @Binding var tab: ConvTab
    let changes: Int

    var body: some View {
        HStack(spacing: 2) {
            item(.chat, L("对话"), count: 0)
            item(.changes, L("改动"), count: changes)
        }
        .padding(2)
        .background(Color.madRaise, in: RoundedRectangle(cornerRadius: 9, style: .continuous))
    }

    private func item(_ t: ConvTab, _ title: String, count: Int) -> some View {
        Button { tab = t } label: {
            HStack(spacing: 5) {
                Text(title)
                if count > 0 { Text(verbatim: "\(count)").font(.mad(11)).monospacedDigit().foregroundStyle(Color.madFg3) }
            }
            .font(.mad(12.5))
            .foregroundStyle(tab == t ? Color.madFg : Color.madFg2)
            .padding(.horizontal, 12)
            .padding(.vertical, 3)
            .background {
                if tab == t {
                    RoundedRectangle(cornerRadius: 7, style: .continuous).fill(Color.madPaper).shadow(color: .black.opacity(0.08), radius: 1, y: 1)
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(tab == t ? .isSelected : [])
    }
}

/// The head's second line: `claude · mad / main · $0.84`, the place in mono.
struct HeadLine: View {
    let model: AgentConversation

    var body: some View {
        var parts: [Text] = []
        if !model.kind.isEmpty { parts.append(Text(model.kind)) }
        if !model.place.isEmpty { parts.append(Text(model.place).font(.mad(11.5, design: .monospaced))) }
        if !model.cost.isEmpty { parts.append(Text(model.cost)) }
        var t = parts.first ?? Text(model.stateLine)
        for p in parts.dropFirst() { t = t + Text(" · ") + p }
        return t.font(.mad(12)).foregroundColor(.madFg3).lineLimit(1)
    }
}

/// ⋯: 在终端打开 (the agent in mad's terminal deck, for the rare case of claude's own menus) and the page.
struct ConvMoreMenu: View {
    let actions: ConversationActions

    var body: some View {
        Menu {
            if let t = actions.openTerminal { Button(L("在终端打开"), action: t) }
            if let w = actions.openWeb { Button(L("在网页里打开"), action: w) }
        } label: {
            Image(systemName: "ellipsis").font(.system(size: 15, weight: .medium)).foregroundStyle(Color.madFg2)
                .frame(width: 30, height: 30)
                .contentShape(Rectangle())
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .menuIndicator(.hidden)
        .fixedSize()
        .accessibilityLabel("更多")
    }
}

#if os(macOS)
/// The Mac's head bar: the avatar and the name with its second line at the left; the tabs, the Details toggle (⌘I)
/// and ⋯ at the right; a hairline under it.
struct ConversationHead: View {
    let model: AgentConversation
    @Binding var tab: ConvTab
    @Binding var detailsOpen: Bool
    let actions: ConversationActions
    @Environment(\.madHeadLeading) private var leading

    var body: some View {
        HStack(spacing: 12) {
            ConversationIdentity(model: model, expands: false)
            Spacer(minLength: 8)
            ConvTabs(tab: $tab, changes: model.details?.changed ?? 0)
            Button { detailsOpen.toggle() } label: {
                Image(systemName: "sidebar.right").font(.system(size: 14)).foregroundStyle(detailsOpen ? Color.madFg : Color.madFg2)
                    .frame(width: 30, height: 30)
                    .background(detailsOpen ? Color.madRaise : Color.clear, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .keyboardShortcut("i", modifiers: .command)
            .help(L("详情 ⌘I"))
            .accessibilityLabel(L("详情"))
            ConvMoreMenu(actions: actions)
        }
        .padding(.leading, 20 + leading)
        .padding(.trailing, 20)
        .padding(.vertical, 12)
        .frame(minHeight: 60)
        .overlay(alignment: .bottom) { Rectangle().fill(Color.madLine).frame(height: 1) }
    }
}
#endif

/// The phone's head: back, the avatar, the name with its second line, ⋯.
struct ConversationHeader: View {
    let model: AgentConversation
    let showsBack: Bool
    let actions: ConversationActions

    var body: some View {
        HStack(spacing: 10) {
            if showsBack {
                Button(action: actions.back) {
                    Image(systemName: "chevron.left")
                        .font(.system(size: 21, weight: .semibold))
                        .foregroundStyle(Color.madAccent)
                        .frame(width: 26, height: 36)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Back")
            }
            ConversationIdentity(model: model)
            ConvMoreMenu(actions: actions)
        }
        .padding(.horizontal, 14)
        .padding(.top, 6)
    }
}

/// The avatar (with its state ring), the name and the second line, read as one.
struct ConversationIdentity: View {
    let model: AgentConversation
    /// Fills the width it is given (the phone's head); the Mac's hugs its text.
    var expands = true

    var body: some View {
        HStack(spacing: 10) {
            CharacterView(id: model.agentID, kind: model.kind, state: model.state, size: 34, dot: model.needsYou)
                .frame(width: 34, height: 34)
            VStack(alignment: .leading, spacing: 0) {
                Text(model.title)
                    .font(.system(size: titleSize, weight: .semibold))
                    .foregroundStyle(Color.madFg)
                    .lineLimit(1)
                HeadLine(model: model)
                PillSubline(text: "", onlyProblems: true)
            }
            if expands { Spacer(minLength: 0) }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(model.title), \(model.stateLine)")
        .accessibilityAddTraits(.isHeader)
    }

    private var titleSize: CGFloat {
        #if os(iOS)
        16
        #else
        15
        #endif
    }
}

/// The 改动 tab: the branch, how many files changed and commits wait to be pushed, and the ways to finish the branch
/// (push, push and open a PR, rebase, merge, the person's own). The diff itself is still the page's: 在网页里看改动.
struct ChangesTab: View {
    let details: AgentDetails?
    let actions: ConversationActions

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                FlowLayout(spacing: 8) {
                    if let d = details {
                        if !d.branch.isEmpty { Text(d.branch).font(.mad(12, design: .monospaced)) }
                        Text(Lf("%lld 个文件", d.changed))
                        if d.ahead > 0 { Text(Lf("↑%lld 未推送", d.ahead)) }
                    }
                }
                .font(.mad(12.5))
                .foregroundStyle(Color.madFg2)
                FlowLayout(spacing: 8) {
                    if let w = actions.openWeb { Button(L("在网页里看改动"), action: w).buttonStyle(.mad()) }
                    if let finish = actions.finish {
                        ForEach(details?.finish ?? []) { b in
                            Button(b.label) { finish(b) }.buttonStyle(.mad(b.primary ? .primary : .normal))
                        }
                    }
                }
                if (details?.changed ?? 0) == 0 {
                    Text(L("没有未提交的改动")).font(.mad(12.5)).foregroundStyle(Color.madFg3)
                }
            }
            .frame(maxWidth: 900, alignment: .leading)
            .padding(.horizontal, 28)
            .padding(.vertical, 22)
            .frame(maxWidth: .infinity)
        }
        .background(Color.madPaper)
    }
}

/// The Mac's Details pane (⌘I): 状态 (now, how long it has run), git (branch, changes, unpushed; the ways to finish the
/// branch), 花费 (this session, tokens in and out, the model), 会话 (its id and directory; Rename, Sleep, Stop).
struct AgentDetailsPane: View {
    let model: AgentConversation
    let actions: ConversationActions
    let rename: () -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                section(L("状态")) {
                    row(L("现在"), model.stateLine)
                    if let since = model.details?.since, !since.isEmpty { row(L("已运行"), since) }
                }
                if let d = model.details {
                    section("git") {
                        row(L("分支"), d.branch.isEmpty ? "—" : d.branch, mono: true)
                        row(L("改动"), Lf("%lld 个文件", d.changed))
                        row(L("未推送"), Lf("%lld 个提交", d.ahead))
                        if let finish = actions.finish, !d.finish.isEmpty {
                            FlowLayout(spacing: 6) {
                                ForEach(d.finish) { b in
                                    Button(b.label) { finish(b) }.buttonStyle(.mad(b.primary ? .primary : .normal, .small))
                                }
                            }
                        }
                    }
                    section(L("花费")) {
                        row(L("本会话"), d.cost.isEmpty ? "—" : d.cost)
                        if !d.tokens.isEmpty { row(L("输入 / 输出"), d.tokens) }
                        row(L("模型"), d.model.isEmpty ? L("默认") : d.model)
                    }
                    section(L("会话")) {
                        row("id", String(d.session.prefix(8)), mono: true)
                        if !d.directory.isEmpty { row(L("目录"), d.directory, mono: true) }
                        FlowLayout(spacing: 6) {
                            if actions.rename != nil { Button(L("改名"), action: rename).buttonStyle(.mad(.normal, .small)) }
                            if let sleep = actions.sleep { Button(L("休眠"), action: sleep).buttonStyle(.mad(.normal, .small)) }
                            Button(L("停止"), action: actions.interrupt).buttonStyle(.mad(.danger, .small))
                        }
                    }
                }
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .background(Color.madSide)
    }

    private func section<Content: View>(_ title: String, @ViewBuilder _ content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title).font(.mad(11.5, weight: .semibold)).foregroundStyle(Color.madFg3)
            content()
        }
        .font(.mad(12.5))
    }

    private func row(_ key: String, _ value: String, mono: Bool = false) -> some View {
        HStack(spacing: 8) {
            Text(key).foregroundStyle(Color.madFg3)
            Spacer(minLength: 8)
            Text(value)
                .font(mono ? Font.mad(12, design: .monospaced) : Font.mad(12.5))
                .monospacedDigit()
                .foregroundStyle(Color.madFg)
                .lineLimit(1)
                .truncationMode(.middle)
        }
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
                                .foregroundStyle(Color.madMineFg)
                                .padding(.horizontal, 14)
                                .padding(.vertical, 8)
                                .hugging(maxWidth: columnWidth > 0 ? MadColumn.bubble(in: columnWidth) : 480)
                                .background(MineShape().fill(Color.madMine))
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

/// What waits on the person, at the end of the conversation (the approved design's ask card, v5). A command: an amber
/// head with the agent's avatar, "想运行一条命令" and how long it has waited; the command in mono; Allow (Return),
/// Allow for this session and Deny and explain… (Esc denies), with the keys named at the right on the Mac. A question:
/// its words and its answers. An act that takes words asks for them first.
struct AskCardView: View {
    let card: AskCard
    var agentID = ""
    var agentKind = ""
    let act: (String, String?) -> Void
    @State private var asking: AskAct?
    @State private var text = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                CharacterView(id: agentID, kind: agentKind, state: .needsYou, size: 18)
                Text(card.title).lineLimit(card.permission ? 1 : 3)
                Spacer(minLength: 8)
                if !card.waited.isEmpty {
                    Text(card.waited).font(.mad(11.5)).fontWeight(.regular).foregroundStyle(Color.madFg3)
                }
            }
            .font(.mad(12.5, weight: .semibold))
            .foregroundStyle(Color.madWait)
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.madWaitBg)
            if !card.detail.isEmpty {
                Group {
                    if card.mono {
                        ScrollView(.horizontal, showsIndicators: false) {
                            Text(card.detail).font(.mad(12.5, design: .monospaced)).lineSpacing(3).fixedSize()
                                .textSelection(.enabled)
                                .padding(.horizontal, 12).padding(.vertical, 10)
                        }
                        .background(Color.madCode)
                    } else {
                        Text(card.detail).font(.mad(13)).foregroundStyle(Color.madFg)
                            .fixedSize(horizontal: false, vertical: true)
                            .padding(.horizontal, 12).padding(.vertical, 10)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
                .overlay(alignment: .bottom) { Rectangle().fill(Color.madLine).frame(height: 1) }
            }
            acts
                .padding(.horizontal, 12)
                .padding(.vertical, 9)
        }
        .background(Color.madPaper)
        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Color.madWait.opacity(0.45), lineWidth: 1))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(card.accessibilityLabel)
    }

    @ViewBuilder private var acts: some View {
        if let asking {
            VStack(alignment: .leading, spacing: 8) {
                TextField(asking.label, text: $text, axis: .vertical)
                    .lineLimit(1...4)
                    .textFieldStyle(.plain)
                    .font(.mad(13))
                    .padding(.horizontal, 10).padding(.vertical, 6)
                    .background(Color.madRaise, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                    .onSubmit(sendWords)
                HStack(spacing: 8) {
                    Button(asking.label, action: sendWords).buttonStyle(.mad(.primary))
                    Button(L("取消")) { self.asking = nil; text = "" }.buttonStyle(.mad())
                        .keyboardShortcut(.cancelAction)
                }
            }
        } else {
            HStack(alignment: .center, spacing: 8) {
                FlowLayout(spacing: 8) {
                    ForEach(card.acts) { a in
                        Button(a.label) { choose(a) }
                            .buttonStyle(.mad(a.primary || a.kind == .allow ? .primary : .normal))
                    }
                }
                #if os(macOS)
                if returnAct != nil || escapeAct != nil {
                    Spacer(minLength: 8)
                    HStack(spacing: 4) {
                        if returnAct != nil { Kbd("⏎"); Text(L("允许")) }
                        if escapeAct != nil { Kbd("esc"); Text(L("拒绝")) }
                    }
                    .font(.mad(11.5))
                    .foregroundStyle(Color.madFg3)
                    .fixedSize()
                }
                #endif
            }
            .background {
                // Return allows and Esc denies, as the hint says (a focused field takes its own Return first)
                if let a = returnAct {
                    Button("") { choose(a) }.keyboardShortcut(.defaultAction).opacity(0).frame(width: 0, height: 0).accessibilityHidden(true)
                }
                if let a = escapeAct {
                    Button("") { choose(a) }.keyboardShortcut(.cancelAction).opacity(0).frame(width: 0, height: 0).accessibilityHidden(true)
                }
            }
        }
    }

    /// The act Return does: the card's Allow (a command's).
    private var returnAct: AskAct? { card.permission ? card.acts.first { $0.kind == .allow } : nil }
    /// The act Esc does: Deny, else Deny and explain.
    private var escapeAct: AskAct? {
        card.permission ? (card.acts.first { $0.kind == .deny } ?? card.acts.first { $0.kind == .denyWhy }) : nil
    }

    private func choose(_ a: AskAct) {
        if a.input { asking = a } else { act(a.id, nil) }
    }

    private func sendWords() {
        guard let asking else { return }
        act(asking.id, text.trimmingCharacters(in: .whitespacesAndNewlines))
        self.asking = nil
        text = ""
    }
}

#if os(iOS)
/// The phone's ask: a sheet that rises from the bottom in the composer's place. A command: "想运行一条命令" and how long it
/// has waited, the command in mono, full-width Deny | Allow, and a ghost Allow for this session under them. A question:
/// its words and its answers.
struct AskSheet: View {
    let card: AskCard
    let act: (String, String?) -> Void
    @State private var asking: AskAct?
    @State private var text = ""
    @State private var contentHeight: CGFloat = 0
    static let maxHeight: CGFloat = 360

    var body: some View {
        VStack(spacing: 10) {
            Capsule().fill(Color.madRaise2).frame(width: 36, height: 5)
            // at the largest text sizes the sheet would fill the screen: past a height it scrolls inside itself
            ScrollView {
                content.background(GeometryReader { Color.clear.preference(key: AskHeightKey.self, value: $0.size.height) })
            }
            .scrollBounceBehavior(.basedOnSize)
            .frame(height: contentHeight > 0 ? min(contentHeight, Self.maxHeight) : nil)
            .onPreferenceChange(AskHeightKey.self) { contentHeight = $0 }
        }
        .padding(.horizontal, 14)
        .padding(.top, 12)
        .padding(.bottom, 12)
        .background(Color.madPaper)
        .overlay(alignment: .top) { Rectangle().fill(Color.madLine).frame(height: 1) }
        .shadow(color: .black.opacity(0.06), radius: 12, y: -8)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(card.accessibilityLabel)
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                Text(card.title)
                    .font(.mad(14, weight: .semibold))
                    .foregroundStyle(Color.madWait)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 8)
                if !card.waited.isEmpty { Text(card.waited).font(.mad(12)).foregroundStyle(Color.madFg3) }
            }
            if !card.detail.isEmpty {
                Text(card.detail)
                    .font(card.mono ? Font.mad(13, design: .monospaced) : Font.system(size: 15))
                    .foregroundStyle(Color.madFg)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, card.mono ? 12 : 0)
                    .padding(.vertical, card.mono ? 10 : 0)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(card.mono ? Color.madCode : Color.clear, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
            }
            if let asking {
                TextField(asking.label, text: $text, axis: .vertical)
                    .lineLimit(1...4)
                    .textFieldStyle(.plain)
                    .padding(.horizontal, 12).padding(.vertical, 9)
                    .background(Color.madRaise, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                HStack(spacing: 8) {
                    Button(L("取消")) { self.asking = nil; text = "" }.buttonStyle(.mad(.normal, .large, wide: true))
                    Button(asking.label) {
                        act(asking.id, text.trimmingCharacters(in: .whitespacesAndNewlines))
                        self.asking = nil
                        text = ""
                    }
                    .buttonStyle(.mad(.primary, .large, wide: true))
                }
            } else if card.permission {
                HStack(spacing: 8) {
                    if let deny = card.acts.first(where: { $0.kind == .deny }) ?? card.acts.first(where: { $0.kind == .denyWhy }) {
                        Button(L("拒绝")) { choose(deny) }.buttonStyle(.mad(.normal, .large, wide: true))
                    }
                    if let allow = card.acts.first(where: { $0.kind == .allow }) {
                        Button(allow.label) { choose(allow) }.buttonStyle(.mad(.primary, .large, wide: true))
                    }
                }
                if let session = card.acts.first(where: { $0.kind == .allowSession }) {
                    Button(session.label) { choose(session) }.buttonStyle(.mad(.ghost, .large, wide: true))
                }
                ForEach(card.acts.filter { $0.kind == .other }) { a in
                    Button(a.label) { choose(a) }.buttonStyle(.mad(.normal, .large, wide: true))
                }
            } else {
                FlowLayout(spacing: 8) {
                    ForEach(card.acts) { a in
                        Button(a.label) { choose(a) }.buttonStyle(.mad(a.primary ? .primary : .normal, .large))
                    }
                }
            }
        }
    }

    private func choose(_ a: AskAct) {
        if a.input { asking = a } else { act(a.id, nil) }
    }
}
#endif

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
