import PhotosUI
import SwiftUI
#if canImport(UIKit)
import UIKit
#endif

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
    /// Pictures waiting to go with the message, and the picker's state.
    @State private var images: [ChannelDraftImage] = []
    @State private var picking = false
    @State private var picked: [PhotosPickerItem] = []
    @State private var pictureNote: String?
    @FocusState private var composerFocused: Bool
    @State private var tab = ConvTab.chat
    @State private var detailsOpen = false
    @State private var renaming = false
    @State private var newName = ""
    @ScaledMetric(relativeTo: .body) private var avatar: CGFloat = 26
    /// The message column's width: a bubble takes a share of it.
    @State private var columnWidth: CGFloat = 0
    #if os(macOS)
    @State private var keeper = BottomKeeper()
    /// How tall the approval card floating above the composer is (0 when there is none): the list leaves room for it.
    /// Bumped once when the list stops being empty: a new identity, so it is built again (see `messages`).
    @State private var listGeneration = 0
    @State private var askHeight: CGFloat = 0
    #else
    /// The create card's change field is open: the composer gives way to it (see `CreateSheet`).
    @State private var createChanging = false
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

    /// `draft` is what the composer starts with (a first message that did not go out, to send again with a tap).
    public init(model: AgentConversation, actions: ConversationActions = .init(), showsBack: Bool = true, draft: String = "") {
        self.model = model
        self.actions = actions
        self.showsBack = showsBack
        _draft = State(initialValue: draft)
    }

    /// The phone's foot: what waits on the person as a sheet in the composer's place, else the composer. (The Mac's
    /// ask card is at the end of the conversation.)
    @ViewBuilder private var bottomBar: some View {
        #if os(iOS)
        // the approval floats above the composer (design v6); the composer stays under it
        VStack(spacing: 10) {
            if let create = model.create {
                CreateSheet(card: create, changing: $createChanging, act: { actions.act(create.id, $0, $1) })
                    .id(create.id)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            } else if let ask = model.ask {
                AskSheet(card: ask, act: { actions.act(ask.id, $0, $1) })
                    .id(ask.id) // a replacement card starts fresh: what was typed for the old one is not its answer
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }
            if !(createChanging && model.create != nil) { composer }
        }
        #else
        // the approval floats over the composer's top, as wide as the column (design v6). It is an overlay, not a row: its
        // text would otherwise make the window's least height as tall as the card at no width. The list keeps clear of it.
        composer
            .macFootBar()
            .overlay(alignment: .top) {
                if let create = model.create {
                    CreateCardView(card: create, act: { actions.act(create.id, $0, $1) })
                        .id(create.id)
                        .macColumn()
                        .background(GeometryReader { g in
                            Color.clear
                                .onAppear { askHeight = g.size.height }
                                .onChange(of: g.size.height) { _, h in askHeight = h }
                                .onDisappear { askHeight = 0 }
                        })
                        .offset(y: -(askHeight + 12))
                } else if let ask = model.ask {
                    AskCardView(card: ask, agentID: model.agentID, agentKind: model.kind, act: { actions.act(ask.id, $0, $1) })
                        .id(ask.id) // a replacement card starts fresh: what was typed for the old one is not its answer
                        .macColumn()
                        .background(GeometryReader { g in
                            Color.clear
                                .onAppear { askHeight = g.size.height }
                                .onChange(of: g.size.height) { _, h in askHeight = h }
                                .onDisappear { askHeight = 0 }
                        })
                        .offset(y: -(askHeight + 12))
                }
            }
        #endif
    }

    #if os(macOS)
    /// The capsule (the avatar with its state ring, the name, `claude · mad / main`) and, in the corner, 对话 | 改动, the
    /// Details toggle (⌘I) and ⋯.
    private var macTop: some View {
        MacTopBar(cornerWidth: 250) {
            MacTitlePill(title: model.title, sub: macSub) {
                CharacterView(id: model.agentID, kind: model.kind, state: model.state, size: 26, dot: model.needsYou)
                    .frame(width: 26, height: 26)
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("\(model.title), \(model.stateLine)")
            .accessibilityAddTraits(.isHeader)
        } corner: {
            ConvTabs(tab: $tab, changes: model.details?.changed ?? 0)
            MacSymbolButton(symbol: "sidebar.right", label: L("详情"), on: detailsOpen) { detailsOpen.toggle() }
                .keyboardShortcut("i", modifiers: .command)
                .help(L("详情 ⌘I"))
            ConvMoreMenu(actions: actions)
        }
    }

    /// `claude · mad / main`; a bad link says so instead.
    private var macSub: String {
        if let note = linkNote { return note.text }
        return [model.kind, model.place].filter { !$0.isEmpty }.joined(separator: " · ")
    }
    @Environment(\.madLinkNote) private var linkNote
    #endif

    public var body: some View {
        #if os(iOS)
        // Full-bleed: the list keeps its frame when the keyboard comes and goes; the head and the bottom bar are insets of
        // it with a material behind them, so what scrolls under them is blurred, never cut.
        Group {
            if tab == .chat {
                messages
                    .safeAreaInset(edge: .bottom, spacing: 0) { bottomBar.background(alignment: .bottom) { PhoneFootFade() } }
            } else {
                ChangesTab(details: model.details, actions: actions)
            }
        }
        .safeAreaInset(edge: .top, spacing: 0) {
            // Grok's top: the round buttons and the capsule float over the list, which scrolls under a fade
            VStack(spacing: 0) {
                ConversationHeader(model: model, showsBack: showsBack, actions: actions)
                ConvTabs(tab: $tab, changes: model.details?.changed ?? 0)
                    .padding(.bottom, 12)
            }
            .background(alignment: .top) { PhoneHeadFade() }
        }
        .background(Color.madPaper)
        .environment(\.madAvatarBackdrop, Color.madPaper)
        .environment(\.convImageLoader, actions.loadImage)
        .environment(\.madOpenCreated, actions.openCreated)
        #else
        VStack(spacing: 0) {
            if tab == .chat {
                messages
                bottomBar
            } else {
                ChangesTab(details: model.details, actions: actions)
            }
        }
        .background(Color.madPaper)
        // no title bar: the messages scroll under a fade, and the capsule and the round buttons float over it
        // over the window's top edge even when a panel (the inspector) gives the window a title bar inset
        .overlay(alignment: .top) { MacTopFade().ignoresSafeArea(.container, edges: .top) }
        .overlay(alignment: .top) { macTop.ignoresSafeArea(.container, edges: .top) }
        .environment(\.madAvatarBackdrop, Color.madPaper)
        .environment(\.convImageLoader, actions.loadImage)
        .environment(\.madOpenCreated, actions.openCreated)
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

    private var rowGap: CGFloat {
        #if os(iOS)
        14
        #else
        Mac.messageGap
        #endif
    }

    private var messages: some View {
        ScrollViewReader { proxy in
            ScrollView {
                ConvStack(spacing: rowGap) {
                    // consecutive tool calls are one small card
                    ForEach(ConvRow.rows(model.entries)) { row in
                        switch row {
                        case let .entry(entry):
                            EntryView(entry: entry, model: model, avatar: avatar, loadEarlier: actions.loadEarlier).id(entry.id)
                        case let .tools(id, tools):
                            #if os(iOS)
                            AgentRow(model: model, avatar: avatar, hugs: true, showsAvatar: false) { ToolCard(tools: tools) }.id(id)
                            #else
                            AgentRow(model: model, avatar: avatar, hugs: true, showsAvatar: false) { MacToolCard(tools: tools) }.id(id)
                            #endif
                        }
                    }
                    if let live = model.live {
                        AgentRow(model: model, avatar: avatar, hugs: true, wide: EntryView.isLong(live.markdown)) { LiveBubbleView(live: live) }
                            .id("live")
                    }
                    #if os(macOS)
                    // room for the approval card that floats over the list's foot, above the end mark: following the end
                    // then shows the last entry above the card, not under it
                    Color.clear.frame(height: askHeight > 0 ? askHeight + 12 : 0)
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
                .padding(.top, 76).padding(.bottom, 14)
                #else
                .padding(.vertical, 8)
                #endif
            }
            .defaultScrollAnchor(.bottom)
            .pinnedToBottomThroughResizes()
            // A conversation that opens empty (a new agent, nothing cached) and then gets its first entries is built
            // again: the lazy stack, scrolled to its end from nothing, otherwise draws no rows until it is rebuilt. The
            // rebuild waits for the next turn of the run loop: done in the layout pass that brought the entries, it
            // changed state while AppKit laid the window out.
            .id(listGeneration)
            .onChange(of: model.entries.isEmpty) { was, now in
                if was && !now { DispatchQueue.main.async { listGeneration += 1 } }
            }
            .scrollDismissesKeyboardIfAvailable()
            .onChange(of: model.entries.count) { _, _ in follow(proxy) }
            .onChange(of: model.live) { _, _ in follow(proxy) }
            .onChange(of: model.ask) { _, _ in follow(proxy) }
            #if os(macOS)
            // the approval card's height is known only once it is drawn: the list's foot grows by it, so follow again
            .onChange(of: askHeight) { _, _ in DispatchQueue.main.async { follow(proxy) } }
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
            #if os(iOS)
            composerWithPictures // the floating capsule has its own edges
            #else
            composerWithPictures
            // the message column's edges, exactly
            .macColumn()
            .padding(.bottom, 22)
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

    /// The pictures and a note above the field, then the field.
    private var composerWithPictures: some View {
        VStack(alignment: .leading, spacing: 6) {
            if !images.isEmpty { DraftImageStrip(images: $images).padding(.horizontal, 18) }
            conversationField
            if let pictureNote { Text(pictureNote).font(MadType.small).foregroundStyle(Color.madDanger).padding(.horizontal, 18) }
        }
        .pastesPictures(focused: composerFocused) { addPicture($0) }
        .photosPicker(isPresented: $picking, selection: $picked, maxSelectionCount: max(1, ChannelLimits.images - images.count), matching: .images)
        .onChange(of: picked) { _, items in loadPicked(items) }
    }

    /// ＋, the field and send: on the Mac one rounded bar with a hairline and a soft shadow, on the phone Grok's floating
    /// capsule.
    @ViewBuilder private var conversationField: some View {
        #if os(macOS)
        // design v6: a floating capsule (54pt, the float fill, a soft shadow): + at the left, send at the right
        HStack(alignment: .bottom, spacing: 12) {
            attachButton
            composerField.padding(.vertical, 7)
            trailing
        }
        .padding(.leading, 10).padding(.trailing, 8).padding(.vertical, 9)
        .frame(minHeight: Mac.composerHeight)
        .background(Color.macFloatFill, in: RoundedRectangle(cornerRadius: Mac.composerHeight / 2, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Mac.composerHeight / 2, style: .continuous).strokeBorder(Color.madLine, lineWidth: 1))
        .macFloat()
        #else
        PhoneComposerCapsule {
            attachButton
            composerField.padding(.vertical, 8)
            trailing
        }
        #endif
    }

    private var attachButton: some View {
        Menu {
            Button { picking = true } label: { Label(L("照片"), systemImage: "photo") }
            #if os(iOS)
            Button { pasteFromPasteboard() } label: { Label(L("粘贴图片"), systemImage: "doc.on.clipboard") }
            #endif
        } label: {
            Image(systemName: "plus")
                #if os(iOS)
                .font(.system(size: 19, weight: .medium))
                .foregroundStyle(Color.madFg)
                .frame(width: 42, height: 42)
                .background(Color.madFg.opacity(0.07), in: Circle())
                #else
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(Color.madFg)
                .frame(width: 34, height: 34)
                .background(Circle().fill(Color.madRaise))
                #endif
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .menuIndicator(.hidden)
        .disabled(images.count >= ChannelLimits.images)
        .accessibilityLabel("Attach")
    }

    /// Takes a picture's bytes: made a type the server keeps, and refused past the limit or too large.
    private func addPicture(_ data: Data) {
        guard images.count < ChannelLimits.images else { pictureNote = Lf("一条消息最多 %lld 张图片。", ChannelLimits.images); return }
        guard let d = ChannelImage.draft(from: data) else { pictureNote = L("这张图片放不进去（不是图片，或太大）。"); return }
        pictureNote = nil
        images.append(d)
    }

    private func loadPicked(_ items: [PhotosPickerItem]) {
        guard !items.isEmpty else { return }
        Task { @MainActor in
            for item in items {
                if let data = try? await item.loadTransferable(type: Data.self) { addPicture(data) }
            }
            picked = []
        }
    }

    #if os(iOS)
    private func pasteFromPasteboard() {
        guard let image = UIPasteboard.general.image, let data = image.pngData() else { pictureNote = L("剪贴板里没有图片。"); return }
        addPicture(data)
    }
    #endif

    private var composerField: some View {
        TextField(model.composerPrompt, text: $draft, axis: .vertical)
            .lineLimit(1...5)
            .textFieldStyle(.plain)
            #if os(iOS)
            .font(Phone.message)
            #else
            .font(Mac.composerText)
            #endif
            .focused($composerFocused)
            .onSubmit(submit)
    }

    @ViewBuilder private var trailing: some View {
        if model.showsInterrupt(draft: draft, hasImages: !images.isEmpty) {
            Button(action: actions.interrupt) {
                Image(systemName: "stop.fill")
                    .font(.mad(14, weight: .bold))
                    .foregroundStyle(.white)
                    .frame(width: sendSize, height: sendSize)
                    .background(Circle().fill(Color(hex: 0xff3b30)))
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Interrupt")
        } else {
            Button(action: submit) {
                Image(systemName: "arrow.up")
                    .font(.system(size: 17, weight: .bold))
                    .foregroundStyle(sendTint)
                    .frame(width: sendSize, height: sendSize)
                    .background(Circle().fill(sendFill))
            }
            .buttonStyle(.plain)
            .disabled(!model.canSubmit(draft: draft, hasImages: !images.isEmpty))
            .accessibilityLabel("Send")
        }
    }

    private var sendSize: CGFloat {
        #if os(iOS)
        42
        #else
        36
        #endif
    }

    private var sendTint: Color {
        model.canSubmit(draft: draft, hasImages: !images.isEmpty) ? Color.madPaper : Color.madFg2
    }

    /// Grok's grey round button that turns black when there is something to send (the Mac's too, design v6).
    private var sendFill: Color {
        model.canSubmit(draft: draft, hasImages: !images.isEmpty) ? Color.madFg : Color.madRaise
    }

    private func submit() {
        guard model.canSubmit(draft: draft, hasImages: !images.isEmpty) else { return }
        actions.send(draft.trimmingCharacters(in: .whitespacesAndNewlines), images)
        draft = ""
        images = []
        picked = []
        pictureNote = nil
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
        #if os(iOS)
        UnevenRoundedRectangle(topLeadingRadius: Phone.bubbleRadius, bottomLeadingRadius: Phone.bubbleRadius, bottomTrailingRadius: 8,
                               topTrailingRadius: Phone.bubbleRadius, style: .continuous).path(in: rect)
        #else
        UnevenRoundedRectangle(topLeadingRadius: Mac.bubbleRadius, bottomLeadingRadius: Mac.bubbleRadius, bottomTrailingRadius: 6,
                               topTrailingRadius: Mac.bubbleRadius, style: .continuous).path(in: rect)
        #endif
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
        .padding(radiusPad)
        .background(Color.madRaise, in: RoundedRectangle(cornerRadius: outer, style: .continuous))
    }

    #if os(iOS)
    // Grok-sized: a 14pt label in a 16pt capsule-ish track
    private let radiusPad: CGFloat = 3
    private let outer: CGFloat = 17
    private let inner: CGFloat = 14
    private let labelFont = Font.system(size: 14)
    private let countFont = Font.system(size: 12)
    private let hPad: CGFloat = 18
    private let vPad: CGFloat = 5
    #else
    // design v6: a 32pt track, 14pt labels
    private let radiusPad: CGFloat = 2
    private let outer: CGFloat = 10
    private let inner: CGFloat = 8
    private let labelFont = Font.system(size: 14)
    private let countFont = Font.system(size: 12.5)
    private let hPad: CGFloat = 13
    private let vPad: CGFloat = 5
    #endif

    private func item(_ t: ConvTab, _ title: String, count: Int) -> some View {
        Button { tab = t } label: {
            HStack(spacing: 5) {
                Text(title)
                if count > 0 { Text(verbatim: "\(count)").font(countFont).monospacedDigit().foregroundStyle(Color.madFg3) }
            }
            .font(labelFont)
            .fontWeight(tab == t ? .semibold : .regular)
            .foregroundStyle(tab == t ? Color.madFg : Color.madFg2)
            .padding(.horizontal, hPad)
            .padding(.vertical, vPad)
            .background {
                if tab == t {
                    RoundedRectangle(cornerRadius: inner, style: .continuous).fill(Color.madPaper).shadow(color: .black.opacity(0.08), radius: 1, y: 1)
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(tab == t ? .isSelected : [])
    }
}

/// ⋯: 在终端打开 (the agent in mad's terminal deck, for the rare case of claude's own menus) and the page.
/// The list's rows, one under another. On the Mac all of them are built: a lazy stack under a scroll view anchored at
/// its bottom re-placed its rows without end as their guessed heights gave way to the real ones (the window hung at
/// full CPU), and a conversation is a few hundred rows at most (the server sends the last 600 entries). The phone keeps
/// the lazy one.
struct ConvStack<Content: View>: View {
    var spacing: CGFloat
    @ViewBuilder var content: Content

    var body: some View {
        #if os(macOS)
        VStack(alignment: .leading, spacing: spacing) { content }
        #else
        LazyVStack(alignment: .leading, spacing: spacing) { content }
        #endif
    }
}

struct ConvMoreMenu: View {
    let actions: ConversationActions
    /// The phone's capsule has no room for it: the menu says it.
    var cost = ""

    var body: some View {
        Menu {
            if let t = actions.openTerminal { Button(L("在终端打开"), action: t) }
            if let w = actions.openWeb { Button(L("在网页里打开"), action: w) }
            #if os(iOS)
            if !cost.isEmpty { Section { Text(cost) } }
            #endif
        } label: {
            #if os(iOS)
            Image(systemName: "ellipsis").font(.system(size: 19, weight: .medium)).foregroundStyle(Color.madFg)
                .frame(width: Phone.button, height: Phone.button)
                .background(Color.phoneFloatFill, in: Circle())
                .contentShape(Circle())
                .phoneFloat()
            #else
            Image(systemName: "ellipsis").font(.system(size: 14, weight: .medium)).foregroundStyle(Color.madFg)
                .frame(width: Mac.button, height: Mac.button)
                .background(Color.macFloatFill, in: Circle())
                .contentShape(Circle())
                .macFloat()
            #endif
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .menuIndicator(.hidden)
        .fixedSize()
        .accessibilityLabel("更多")
    }
}

#if os(iOS)
/// The phone's head (design v6): the round back button, the capsule with the avatar, the name and `claude · mad / main`,
/// and the round ⋯.
struct ConversationHeader: View {
    let model: AgentConversation
    let showsBack: Bool
    let actions: ConversationActions
    @Environment(\.madLinkNote) private var note

    var body: some View {
        PhoneTopBar {
            if showsBack { PhoneBackButton(action: actions.back) } else { Color.clear.frame(width: Phone.button, height: Phone.button) }
        } center: {
            PhoneTitlePill(title: model.title, sub: sub) {
                CharacterView(id: model.agentID, kind: model.kind, state: model.state, size: 32, dot: model.needsYou)
                    .frame(width: 32, height: 32)
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("\(model.title), \(model.stateLine)")
            .accessibilityAddTraits(.isHeader)
        } trailing: {
            ConvMoreMenu(actions: actions, cost: model.cost)
        }
    }

    /// `claude · mad / main`; a bad link says so instead.
    private var sub: String {
        if let note { return note.text }
        return [model.kind, model.place].filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

#endif

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
                        if !d.branch.isEmpty { Text(d.branch).font(.mad(macOnly: 14, 12, design: .monospaced)) }
                        Text(Lf("%lld 个文件", d.changed))
                        if d.ahead > 0 { Text(Lf("↑%lld 未推送", d.ahead)) }
                    }
                }
                .font(.mad(macOnly: 14, 12.5))
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
                    Text(L("没有未提交的改动")).font(.mad(macOnly: 14, 12.5)).foregroundStyle(Color.madFg3)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 28)
            #if os(macOS)
            .padding(.top, 76).padding(.bottom, 22) // under the floating capsule
            #else
            .padding(.vertical, 22)
            #endif
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
    /// The phone draws the small character at a bubble's bottom-left; a tool card or a picture keeps the gutter empty.
    var showsAvatar = true
    /// The Mac: an agent's long answer may take 700 instead of 600.
    var wide = false
    /// The Mac: what the hover buttons copy (nil: none, a line of tools or a picture).
    var copyText: String?
    @ViewBuilder var content: Content
    @Environment(\.madColumnWidth) private var columnWidth

    var body: some View {
        #if os(iOS)
        HStack(alignment: .bottom, spacing: Phone.chatSide - 6) {
            Group {
                if showsAvatar {
                    CharacterView(id: model.agentID, kind: model.kind, state: .idle, size: Phone.chatAvatar)
                        .accessibilityHidden(true)
                } else {
                    Color.clear
                }
            }
            .frame(width: Phone.chatAvatar, height: Phone.chatAvatar)
            Group {
                if hugs && columnWidth > 0 { content.hugging(maxWidth: MadColumn.bubble(in: columnWidth)) } else { content }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        #else
        // Grok's chat: the small character at the bubble's bottom-left (a tool card or a picture keeps the gutter empty)
        HStack(alignment: .bottom, spacing: MadColumn.gutter - Mac.chatAvatar) {
            Group {
                if showsAvatar {
                    CharacterView(id: model.agentID, kind: model.kind, state: .idle, size: Mac.chatAvatar)
                        .accessibilityHidden(true)
                } else {
                    Color.clear
                }
            }
            .frame(width: Mac.chatAvatar, height: Mac.chatAvatar)
            let bubble = Group {
                if hugs && columnWidth > 0 {
                    content.hugging(maxWidth: wide ? MadColumn.wideBubble(in: columnWidth) : MadColumn.bubble(in: columnWidth))
                } else {
                    content
                }
            }
            if let copyText {
                MacHoverRow(side: .leading, copyText: copyText) { bubble }
            } else {
                bubble.frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        #endif
    }
}

struct EntryView: View {
    let entry: ConvEntry
    let model: AgentConversation
    let avatar: CGFloat
    let loadEarlier: (() -> Void)?
    @Environment(\.madColumnWidth) private var columnWidth
    @Environment(\.madOpenCreated) private var openCreated

    #if os(iOS)
    private var bodyFont: Font { Phone.message }
    private var lineSpacing: CGFloat { Phone.messageLineSpacing }
    private var minePadding: EdgeInsets { Phone.bubblePadding }
    #else
    private var bodyFont: Font { MadType.message }
    private var lineSpacing: CGFloat { MadType.messageLineSpacing }
    private var minePadding: EdgeInsets { Mac.bubblePadding }
    #endif

    /// A long answer (a few paragraphs, or code) takes the wide bubble on the Mac.
    static func isLong(_ markdown: String) -> Bool {
        markdown.count > 220 || markdown.contains("```") || markdown.contains("\n\n")
    }

    var body: some View {
        Group {
            switch entry.content {
            case let .user(text, images):
                HStack(spacing: 0) {
                    Spacer(minLength: MadColumn.gutter)
                    VStack(alignment: .trailing, spacing: 4) {
                        ForEach(Array(images.enumerated()), id: \.offset) { ImageView(image: $0.element) }
                        if !text.isEmpty {
                            let mine = Text(text)
                                .font(bodyFont)
                                .lineSpacing(lineSpacing)
                                .fixedSize(horizontal: false, vertical: true)
                                .foregroundStyle(Color.madMineFg)
                                .padding(minePadding)
                                .hugging(maxWidth: columnWidth > 0 ? MadColumn.bubble(in: columnWidth) : 480)
                                .background(MineShape().fill(Color.madMine))
                            #if os(macOS)
                            MacHoverRow(side: .trailing, copyText: text) { mine }
                            #else
                            mine
                            #endif
                        }
                    }
                }
            case let .text(markdown):
                AgentRow(model: model, avatar: avatar, hugs: true, wide: Self.isLong(markdown), copyText: markdown) { MarkdownBubble(markdown: markdown) }
            case let .tool(tool):
                AgentRow(model: model, avatar: avatar, hugs: true, showsAvatar: false) { ToolRow(tool: tool) }
            case let .image(image):
                AgentRow(model: model, avatar: avatar, showsAvatar: false) { ImageView(image: image) }
            case let .create(line):
                CreateLineView(line: line, open: openCreated)
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
        } else if case .create = entry.content {
            content // its own elements: a result line is one button, a note is its text
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
        VStack(alignment: .leading, spacing: stackSpacing) {
            ForEach(Array(Markdown.blocks(markdown).enumerated()), id: \.offset) { _, block in
                switch block {
                case let .text(s): Text(Self.attributed(s)).font(bodyFont).lineSpacing(lineSpacing)
                case let .code(s): CodeBlock(code: s)
                case let .table(header, rows): TableBlock(header: header, rows: rows) { Self.attributed($0) }
                }
            }
        }
        .textSelection(.enabled)
        #if os(iOS)
        .padding(Phone.bubblePadding)
        .background(RoundedRectangle(cornerRadius: Phone.bubbleRadius, style: .continuous).fill(Color.chatBubble))
        #else
        .padding(Mac.bubblePadding)
        .background(RoundedRectangle(cornerRadius: Mac.bubbleRadius, style: .continuous).fill(Color.chatBubble))
        #endif
    }

    #if os(iOS)
    private var stackSpacing: CGFloat { 8 }
    private var bodyFont: Font { Phone.message }
    private var lineSpacing: CGFloat { Phone.messageLineSpacing }
    #else
    private var stackSpacing: CGFloat { 10 }
    private var bodyFont: Font { MadType.message }
    private var lineSpacing: CGFloat { MadType.messageLineSpacing }
    #endif

    static func attributed(_ s: String) -> AttributedString {
        (try? AttributedString(markdown: s, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(s)
    }
}

/// A pipe table: a bold header over a rule, a line between rows, columns as wide as their widest cell; a wide one
/// scrolls sideways inside the bubble. `cell` reads a cell's inline markdown.
struct TableBlock: View {
    let header: [String]
    let rows: [[String]]
    let cell: (String) -> AttributedString

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Grid(alignment: .leading, horizontalSpacing: 18, verticalSpacing: 7) {
                GridRow {
                    ForEach(Array(header.enumerated()), id: \.offset) { _, h in Text(cell(h)).bold() }
                }
                Divider().gridCellUnsizedAxes(.horizontal)
                ForEach(Array(rows.enumerated()), id: \.offset) { i, row in
                    if i > 0 { Divider().opacity(0.5).gridCellUnsizedAxes(.horizontal) }
                    GridRow {
                        ForEach(Array(row.enumerated()), id: \.offset) { _, c in Text(cell(c)) }
                    }
                }
            }
            #if os(iOS)
            .font(Phone.message)
            #else
            .font(MadType.message)
            #endif
            .fixedSize()
            .padding(.vertical, 2)
        }
    }
}

/// Code scrolls sideways inside its own box; it never widens the bubble.
struct CodeBlock: View {
    let code: String

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Text(code)
                #if os(iOS)
                .font(Phone.code)
                #else
                .font(MadType.code)
                #endif
                .fixedSize()
                #if os(iOS)
                .padding(10)
                #else
                .lineSpacing(4)
                .padding(.horizontal, 13).padding(.vertical, 11)
                #endif
        }
        #if os(iOS)
        .background(RoundedRectangle(cornerRadius: 14, style: .continuous).fill(Color.madCode))
        #else
        .background(RoundedRectangle(cornerRadius: 11, style: .continuous).fill(Color.primary.opacity(0.07)))
        #endif
    }
}

struct LiveBubbleView: View {
    let live: LiveBubble

    var body: some View {
        Group {
            if live.markdown.isEmpty {
                #if os(iOS)
                ProgressView().controlSize(.small).padding(.horizontal, 20).padding(.vertical, 16)
                    .background(RoundedRectangle(cornerRadius: Phone.bubbleRadius, style: .continuous).fill(Color.chatBubble))
                #else
                ProgressView().controlSize(.small).padding(.horizontal, 18).padding(.vertical, 16)
                    .background(RoundedRectangle(cornerRadius: Mac.bubbleRadius, style: .continuous).fill(Color.chatBubble))
                #endif
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

struct DetailBox: View {
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

#if os(iOS)
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

#endif

#if os(iOS)
/// The phone's ask (design v6): a floating white card above the composer, 28pt corners and a soft shadow. A command:
/// "想运行一条命令" in amber and how long it has waited, the whole command in a mono box that wraps at spaces, big
/// Deny | Allow pills (Allow black) and a quiet Allow for this session under them. A question: its words and its answers.
struct AskSheet: View {
    let card: AskCard
    let act: (String, String?) -> Void
    @State private var asking: AskAct?
    /// The act sent and waiting for the Mac (AskChips draws it).
    @State private var sent: String?
    @State private var text = ""
    @State private var contentHeight: CGFloat = 0
    static let maxHeight: CGFloat = 420
    @Environment(\.colorScheme) private var scheme
    /// White in light mode; in dark mode the paper would vanish into the page, so the card is one step lighter.
    private var cardFill: Color { scheme == .dark ? Color.madRaise : Color.madPaper }

    var body: some View {
        // at the largest text sizes the card would fill the screen: past a height it scrolls inside itself
        ScrollView {
            content.background(GeometryReader { Color.clear.preference(key: AskHeightKey.self, value: $0.size.height) })
        }
        .scrollBounceBehavior(.basedOnSize)
        .frame(height: contentHeight > 0 ? min(contentHeight, Self.maxHeight) : nil)
        .onPreferenceChange(AskHeightKey.self) { contentHeight = $0 }
        .padding(.horizontal, 18)
        .padding(.top, 18)
        .padding(.bottom, 14)
        .background {
            RoundedRectangle(cornerRadius: Phone.cardRadius, style: .continuous).fill(cardFill)
                .shadow(color: Color(red: 15 / 255, green: 18 / 255, blue: 30 / 255).opacity(0.16), radius: 20, x: 0, y: 10)
                .shadow(color: Color(red: 15 / 255, green: 18 / 255, blue: 30 / 255).opacity(0.08), radius: 1.5, x: 0, y: 1)
        }
        .padding(.horizontal, 12)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(card.accessibilityLabel)
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(card.title)
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundStyle(Color.madWait)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 8)
                if !card.waited.isEmpty { Text(card.waited).font(.system(size: 14)).foregroundStyle(Color.madFg3) }
            }
            if !card.detail.isEmpty {
                Text(card.detail)
                    .font(card.mono ? Phone.code : Phone.body)
                    .lineSpacing(card.mono ? 4 : 3)
                    .foregroundStyle(Color.madFg)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
                    .padding(.horizontal, card.mono ? 14 : 0)
                    .padding(.vertical, card.mono ? 12 : 0)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(card.mono ? (scheme == .dark ? Color.madCode : Color.madCode) : Color.clear, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            }
            if let asking {
                TextField(asking.label, text: $text, axis: .vertical)
                    .lineLimit(1...4)
                    .textFieldStyle(.plain)
                    .font(Phone.body)
                    .padding(.horizontal, 14).padding(.vertical, 12)
                    .background(Color.madRaise, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                HStack(spacing: 10) {
                    Button(L("取消")) { self.asking = nil; text = "" }.buttonStyle(AskPill(filled: false))
                    Button(asking.label) {
                        act(asking.id, text.trimmingCharacters(in: .whitespacesAndNewlines))
                        self.asking = nil
                        text = ""
                    }
                    .buttonStyle(AskPill(filled: true))
                }
            } else {
                AskChips(card: card, send: { act($0.id, nil) }, input: { asking = $0 }, sent: $sent)
            }
        }
    }
}

/// A big pill of the approval card: 50pt, Allow (filled) black, the rest outlined.
private struct AskPill: ButtonStyle {
    let filled: Bool
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 17, weight: .semibold))
            .lineLimit(2)
            .multilineTextAlignment(.center)
            .foregroundStyle(filled ? Color.madPaper : Color.madFg)
            .frame(maxWidth: .infinity, minHeight: 50)
            .padding(.horizontal, 12)
            .background(filled ? Color.madFg : Color.madPaper, in: Capsule())
            .overlay(Capsule().strokeBorder(filled ? Color.madFg : Color.madLine2, lineWidth: 1))
            .opacity(configuration.isPressed ? 0.7 : 1)
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

extension Font {
    /// `size` on the Mac (the v6 scale), `phone` through the usual mapping on the iPhone, which does not change.
    static func mad(macOnly size: CGFloat, _ phone: CGFloat, weight: Font.Weight = .regular, design: Font.Design = .default) -> Font {
        #if os(macOS)
        return .system(size: size, weight: weight, design: design)
        #else
        return .mad(phone, weight: weight, design: design)
        #endif
    }
}
