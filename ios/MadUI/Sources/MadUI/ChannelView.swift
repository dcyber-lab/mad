import SwiftUI

/// A channel, as the approved mockup (v4) draws it: a one-line head (back, the tile, the name and how
/// many work, ⓘ), quiet messages (grey bubbles for agents, blue for the person, runs under one name,
/// a line of time after a quiet while, reports folded to a line), a one-line composer, and the 群信息
/// panel, which slides in from the right (a swipe from the right edge, or the name, or ⓘ); on the
/// Mac it is an inspector at the trailing side. It draws what `screen` says and asks `actions` for the rest.
public struct ChannelView: View {
    let screen: ChannelScreen
    let actions: ChannelActions

    @State private var panelOpen = false
    @State private var gallery = false
    @State private var nearBottom = true
    @State private var viewport = CGSize.zero
    #if os(iOS)
    // The list is full-bleed: the head, the composer and the keyboard are insets over it, and the end marker is still
    // realized while it sits under them. The end is judged by where the marker's foot is against the visible part.
    @State private var markerShown = true
    @State private var markerFoot: CGFloat = 0
    @State private var bottomInset: CGFloat = 0
    /// The list was at its end when it was last judged with the same viewport and inset: what a size change finds it as,
    /// whatever the change itself did to the marker (a marker pushed out by the keyboard is not a reader who scrolled up).
    @State private var endLatch = true
    @State private var judgedViewport: CGFloat = -1
    @State private var judgedInset: CGFloat = -1
    #endif
    #if os(macOS)
    // The Mac's window is resized by the person and keeps its list at the bottom through that (see `BottomWatch`).
    @State private var keeper = BottomKeeper()
    @State private var watch = BottomWatch()
    /// How many rows the lazy list has made now (not state: counting must not draw the view again).
    @State private var realized = RealizedRows()
    #endif
    @State private var expanded: Set<Int> = []
    /// How many of the screen's last items the list holds. A channel keeps hundreds of rows (the device's cache holds 200
    /// messages, some with pictures). A lazy list of them, opened at its bottom, has to guess the heights of the rows above
    /// the screen: the bottom anchor lands on a guess, and the list shows an empty band with the way-down button until
    /// a scroll corrects it. So the list holds the last `Self.firstRows` in a plain stack (whose height is exact, and the
    /// bottom anchor lands where it says) and takes `Self.moreRows` more when the person asks for them.
    @State private var rowsHeld = ChannelView.firstRows
    @State private var replying: ChannelQuote?
    @State private var viewing: String?
    @State private var drag: CGFloat = 0
    /// The message area's width: the right-edge swipe starts in its last 22 points.
    @State private var chatWidth: CGFloat = 0
    /// The message column's width (inside its side padding): a bubble takes a share of it.
    @State private var columnWidth: CGFloat = 0

    /// panelOpen: whether the 群信息 panel is open when the view first shows.
    public init(screen: ChannelScreen, actions: ChannelActions, panelOpen: Bool = false) {
        self.screen = screen
        self.actions = actions
        _panelOpen = State(initialValue: panelOpen)
    }

    private static let bottom = "bottom"
    static let firstRows = 40
    static let moreRows = 40
    private static let space = "channelList"

    public var body: some View {
        #if os(macOS)
        chat
            .ignoresSafeArea(.container, edges: .top) // the head bar starts at the window's top, as the conversation's
            .environment(\.madAvatarBackdrop, Color.madPaper)
            .inspector(isPresented: $panelOpen) { panelView.inspectorColumnWidth(min: 260, ideal: 300, max: 380) }
            .sheet(isPresented: $gallery) { galleryView }
            .sheet(item: Binding(get: { viewing.map(Picked.init) }, set: { viewing = $0?.id })) { p in
                PictureViewer(id: p.id, load: actions.loadPicture) { viewing = nil }
            }
        #else
        chat
            // While the panel is open it is the page: the chat is out of the VoiceOver tree.
            .accessibilityHidden(panelOpen)
            .overlay { phonePanel }
            .sheet(isPresented: $gallery) { galleryView }
            .fullScreenCover(item: Binding(get: { viewing.map(Picked.init) }, set: { viewing = $0?.id })) { p in
                PictureViewer(id: p.id, load: actions.loadPicture) { viewing = nil }
            }
        #endif
    }

    private struct Picked: Identifiable { let id: String }

    // MARK: the chat

    private var composerRow: some View {
        // the composer has the message column's edges, exactly
        ChannelComposer(placeholder: screen.placeholder, readOnly: screen.readOnly, mentions: screen.panel.members + screen.panel.helpers,
                        replying: $replying, send: actions.send)
            .frame(maxWidth: MadColumn.maxWidth)
            #if os(macOS)
            .padding(.horizontal, MadColumn.side)
            #endif
            .frame(maxWidth: .infinity)
    }

    private var chat: some View {
        #if os(iOS)
        // The list is full-bleed and never changes its frame: the head and the composer are insets of it, so the keyboard
        // changes the list's inset (the list stays where it is under it), a composer that grows goes up over the list, and
        // what scrolls under the head is blurred by its material, not cut.
        messages
            .safeAreaInset(edge: .top, spacing: 0) { head.background(alignment: .top) { PhoneHeadFade() } }
            .safeAreaInset(edge: .bottom, spacing: 0) { composerRow.background(alignment: .bottom) { PhoneFootFade() } }
            .background(Color.madPaper)
        #else
        // no title bar (design v6): the list scrolls under a fade, the capsule and the round button float over it, and the
        // composer floats over the list's foot
        VStack(spacing: 0) {
            messages
            composerRow.macFootBar()
        }
        .background(Color.madPaper)
        // over the window's top edge even when a panel (the inspector) gives the window a title bar inset
        .overlay(alignment: .top) { MacTopFade().ignoresSafeArea(.container, edges: .top) }
        .overlay(alignment: .top) { macTop.ignoresSafeArea(.container, edges: .top) }
        #endif
    }

    #if os(iOS)
    /// The phone's head (design v6): the round back button, the capsule with the group's tile, its name and
    /// "3 个成员 · 1 个在工作" (it opens 群信息), and the round ⋯ (also 群信息).
    private var head: some View {
        PhoneTopBar {
            if let back = actions.back { PhoneBackButton { back() } } else { Color.clear.frame(width: Phone.button, height: Phone.button) }
        } center: {
            Button { panelOpen = true } label: {
                PhoneTitlePill(title: screen.title, sub: pillSub) {
                    GroupTile(members: screen.members.filter { !$0.dim }.map { ($0.id, $0.kind, $0.state) }, size: 34,
                              dot: (screen.members + screen.helpers).contains { $0.state == .needsYou })
                        .frame(width: 34, height: 34)
                }
                .contentShape(Capsule())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(screen.title)，\(screen.stateLine)")
            .accessibilityHint(L("群信息"))
        } trailing: {
            PhoneRoundButton(action: { panelOpen.toggle() }) { Image(systemName: "ellipsis") }
                .accessibilityLabel(L("群信息"))
        }
    }
    #else
    /// The Mac's top (design v6): the capsule with the group's tile, its name and "3 个成员 · 1 个在工作" (it opens 群信息),
    /// and the round 群信息 toggle in the corner.
    private var macTop: some View {
        MacTopBar(cornerWidth: 60) {
            if let back = actions.back {
                MacSymbolButton(symbol: "chevron.backward", label: L("返回")) { back() }
            }
        } pill: {
            Button { panelOpen = true } label: {
                MacTitlePill(title: screen.title, sub: pillSub) {
                    GroupTile(members: screen.members.filter { !$0.dim }.map { ($0.id, $0.kind, $0.state) }, size: 26,
                              dot: (screen.members + screen.helpers).contains { $0.state == .needsYou })
                        .frame(width: 26, height: 26)
                }
                .contentShape(Capsule())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(screen.title)，\(screen.stateLine)")
            .accessibilityHint(L("群信息"))
        } corner: {
            MacSymbolButton(symbol: "sidebar.right", label: L("群信息"), on: panelOpen) { panelOpen.toggle() }
        }
    }
    #endif

    /// "3 个成员 · 1 个在工作" (a quiet channel says only its count); a bad link says so instead.
    private var pillSub: String {
        if let note { return note.text }
        let live = screen.members.filter { !$0.dim }
        guard !live.isEmpty else { return screen.stateLine }
        let count = Lf("%lld 个成员", live.count)
        // one clause, so the capsule never cuts it: who waits for the person first, else who works
        let needing = (screen.members + screen.helpers).filter { !$0.dim && $0.state == .needsYou }.count
        let working = (screen.members + screen.helpers).filter { !$0.dim && $0.state == .working }.count
        if needing > 0 { return count + " · " + Lf("%lld 个在等你", needing) }
        if working > 0 { return count + " · " + Lf("%lld 个在工作", working) }
        return count
    }
    @Environment(\.madLinkNote) private var note

    private var messages: some View {
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    // Each row is drawn again only when what it shows changed: a look that brought nothing new (or one
                    // new message) leaves the other rows alone.
                    if heldItems.count < screen.items.count {
                        // the top of what is held: it takes the next older rows, and the list stays on the row it was at
                        Button { loadEarlier(proxy) } label: {
                            Text(L("更早的消息")).font(MadType.label).foregroundStyle(.secondary).frame(maxWidth: .infinity).padding(.vertical, 10)
                        }
                        .buttonStyle(.plain)
                    }
                    ForEach(heldItems) { item in
                        DrawnOnChange(item: item, expanded: expandedBubbles(of: item), width: columnWidth) { row(item) }.equatable().id(item.id)
                            #if os(macOS)
                            .onAppear { realized.count += 1 }
                            .onDisappear { realized.count -= 1 }
                            #endif
                    }
                    // The last row of the lazy stack: it appears and disappears as the scroll brings it
                    // in and out of view, so a person who has scrolled up to read is not pulled down.
                    Color.clear.frame(height: 1).id(Self.bottom)
                        #if os(iOS)
                        .background(GeometryReader { g in
                            Color.clear.onAppear { markerFoot = g.frame(in: .named(Self.space)).maxY; markerShown = true; judge() }
                                .onChange(of: g.frame(in: .named(Self.space)).maxY) { _, y in markerFoot = y; judge() }
                        })
                        .onDisappear {
                            markerShown = false
                            nearBottom = false
                            // gone with the viewport unchanged: the person scrolled; gone with a resize: the resize did it
                            if judgedViewport == viewport.height && judgedInset == bottomInset { endLatch = false }
                        }
                        #else
                        .background(GeometryReader { g in
                            // where the marker's foot is in the scroll view, kept without drawing the view again
                            Color.clear.onAppear { realized.markerFoot = g.frame(in: .named(Self.space)).maxY }
                                .onChange(of: g.frame(in: .named(Self.space)).maxY) { _, y in realized.markerFoot = y }
                        })
                        .onAppear { watch.appeared(); nearBottom = true }
                        .onDisappear {
                            // a message that arrives pushes the marker out for a moment and the list follows it: that
                            // is judged when the follow is over, so a person who scrolls up in that moment is not missed
                            switch watch.disappeared() {
                            case .now: left(proxy)
                            case .recheck(let after):
                                DispatchQueue.main.asyncAfter(deadline: .now() + after + 0.02) { if watch.stillLeft { left(proxy) } }
                            }
                        }
                        #endif
                }
                .frame(maxWidth: MadColumn.maxWidth)
                .background(GeometryReader { g in
                    Color.clear.onAppear { columnWidth = g.size.width }.onChange(of: g.size.width) { _, w in columnWidth = w }
                })
                .padding(.horizontal, MadColumn.side)
                .frame(maxWidth: .infinity)
                #if os(macOS)
                .padding(.top, 76) // under the floating capsule
                .padding(.bottom, 14)
                #else
                .padding(.top, 6)
                .padding(.bottom, 10)
                #endif
            }
            .overlay(alignment: .top) {
                // an empty channel: one quiet line, a third of the way down the list area (the list itself is bottom-anchored)
                if let line = emptyLine {
                    Text(line).font(MadType.small).foregroundStyle(.secondary).multilineTextAlignment(.center)
                        .padding(.horizontal, 24).padding(.top, max(0, viewport.height / 3)).allowsHitTesting(false)
                }
            }
            .coordinateSpace(name: Self.space)
            .overlay(alignment: .bottom) {
                if !nearBottom {
                    Button { withAnimation { proxy.scrollTo(Self.bottom, anchor: .bottom) } } label: {
                        Image(systemName: "arrow.down").font(.system(size: 14, weight: .semibold))
                            .frame(width: 34, height: 34)
                            .background(.regularMaterial, in: Circle())
                            .overlay(Circle().strokeBorder(Color.chatLine))
                            .shadow(color: .black.opacity(0.08), radius: 6, y: 2)
                    }
                    .buttonStyle(.plain).padding(.bottom, 10).accessibilityLabel(L("到最新"))
                    .transition(.opacity)
                }
            }
            .background(GeometryReader { g in
                Color.clear.onAppear {
                    viewport = g.size
                    #if os(iOS)
                    bottomInset = g.safeAreaInsets.bottom
                    judge() // the marker's foot may have been measured before the viewport was
                    #endif
                }
                #if os(iOS)
                .onChange(of: g.safeAreaInsets.bottom) { _, inset in
                    // the keyboard, the composer growing or the ask card changed what covers the list's foot: a list that was
                    // at its end is put back at its end once (the offset is kept through the change)
                    bottomInset = inset
                    judge()
                    if endLatch { stick(proxy) }
                }
                #endif
                .onChange(of: g.size) { _, size in
                    viewport = size
                    #if os(iOS)
                    judge()
                    if endLatch { stick(proxy) }
                    #endif
                    #if os(macOS)
                    // a list at its bottom stays there as the viewport changes: put back at once, not after a wait
                    if nearBottom || keeper.resized() { stick(proxy) }
                    watch.resized()
                    settle(proxy)
                    #endif
                }
            })
            #if os(iOS)
            .background(GeometryReader { g in
                Color.clear.onAppear { chatWidth = g.size.width }.onChange(of: g.size.width) { _, w in chatWidth = w }
            })
            .simultaneousGesture(edgeOpen)
            #endif
            .defaultScrollAnchor(.bottom)
            .pinnedToBottomThroughResizes() // iOS 18 and the Mac: the system keeps the bottom through size changes
            .scrollDismissesKeyboard(.interactively)
            .onChange(of: screen.tail) { _, _ in
                var mine = false
                if case .mine? = screen.items.last { mine = true }
                // No animation: a lazy list scrolled under an animation while its rows are measured draws
                // gaps, and the new message pushing the marker out would flash the way-down button.
                if nearBottom || mine {
                    #if os(macOS)
                    watch.followed()
                    #endif
                    stick(proxy)
                }
            }
        }
    }

    private var heldItems: ArraySlice<ChannelItem> { screen.items.suffix(rowsHeld) }

    /// The next older rows are held, and the row that was the first stays where it was on screen.
    private func loadEarlier(_ proxy: ScrollViewProxy) {
        guard let first = heldItems.first else { return }
        rowsHeld += Self.moreRows
        DispatchQueue.main.async { proxy.scrollTo(first.id, anchor: .top) }
    }

    /// The ids of the bubbles of `item` that are open: part of what its row shows.
    private func expandedBubbles(of item: ChannelItem) -> [Int] {
        switch item {
        case .mine(let b): expanded.contains(b.id) ? [b.id] : []
        case .run(let r): r.bubbles.map(\.id).filter { expanded.contains($0) }
        default: []
        }
    }

    /// The channel has nothing but notes, one of them an empty-state note (the note of older messages dropped may be
    /// first): its text.
    private var emptyLine: String? {
        var line: String?
        for item in screen.items {
            guard case .note(let id, let text) = item else { return nil }
            if id == "quiet" || id == "empty" { line = text }
        }
        return line
    }

    #if os(macOS)
    /// Once the layout has had time to settle after a resize (the keyboard comes in several steps, a third-party one
    /// with its own height, and the composer grows with it): a list that was at the bottom is at the bottom again,
    /// and a list that shows no row at all (an offset left past the content) is put right whatever it was doing.
    private func settle(_ proxy: ScrollViewProxy) {
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.6) {
            let blank = realized.count <= 0 && !screen.items.isEmpty
            // the marker's foot well above the viewport's: an offset left past the end of the content (the viewport grew
            // as the keyboard went away), an empty band under the last message
            let pastEnd = BottomWatch.pastEnd(markerShown: watch.shown, markerFoot: realized.markerFoot, viewportHeight: viewport.height)
            if blank || pastEnd || watch.leftWithResize { stick(proxy) }
        }
    }

    /// The person is not at the bottom: no more following, and the way-down button shows. A resize that
    /// came with it puts the list back (`BottomKeeper`).
    private func left(_ proxy: ScrollViewProxy) {
        nearBottom = false
        if keeper.leftBottom() { stick(proxy) }
    }

    #endif

    #if os(iOS)
    /// At the end: the marker is realized and its foot is within the list's visible height. (`viewport` is the list's own
    /// frame, which the head, the composer and the keyboard already cut down: a marker realized under them is below it.)
    private func judge() {
        nearBottom = markerShown && markerFoot <= viewport.height + 2
        if judgedViewport == viewport.height && judgedInset == bottomInset { endLatch = nearBottom }
        judgedViewport = viewport.height
        judgedInset = bottomInset
    }
    #endif

    /// Back to the bottom once the layout has settled (after a resize that moved it).
    private func stick(_ proxy: ScrollViewProxy) {
        DispatchQueue.main.async { proxy.scrollTo(Self.bottom, anchor: .bottom) }
    }

    @ViewBuilder private func row(_ item: ChannelItem) -> some View {
        switch item {
        case .time(_, let text):
            #if os(macOS)
            Text(text).font(Mac.date).foregroundStyle(Color.madFg3).frame(maxWidth: .infinity).padding(.top, 14).padding(.bottom, 2)
            #else
            Text(text).font(.mad(11.5)).foregroundStyle(Color.madFg3).frame(maxWidth: .infinity).padding(.top, 14).padding(.bottom, 8)
            #endif
        case .note(let id, _) where id == "quiet" || id == "empty":
            Color.clear.frame(height: 1) // drawn over the list area (see `emptyLine`)
        case .note(_, let text):
            Text(text).font(MadType.label).foregroundStyle(.secondary).multilineTextAlignment(.center).frame(maxWidth: .infinity).padding(.vertical, 8)
        case .mine(let b):
            // against the column's right edge, as wide as its text; under the latest one, the word for how far it got
            VStack(alignment: .trailing, spacing: 4) {
                HStack(spacing: 0) {
                    Spacer(minLength: MadColumn.gutter)
                    bubble(b, mine: true, who: L("自己"))
                }
                if b.lastOwn { ReceiptLine(receipt: b.receipt) }
            }
            #if os(macOS)
            .padding(.top, 14)
            #else
            .padding(.top, 6)
            #endif
        case .run(let run):
            #if os(macOS)
            // Grok's group chat: the sender's name in its colour over the bubbles, the 28pt avatar at the last bubble's bottom-left
            HStack(alignment: .bottom, spacing: MadColumn.gutter - Mac.chatAvatar) {
                CharacterView(id: run.sender.id, kind: run.sender.kind, state: run.sender.state, size: Mac.chatAvatar)
                    .frame(width: Mac.chatAvatar, height: Mac.chatAvatar)
                    .onTapGesture { actions.openAgent(run.sender.id) }
                VStack(alignment: .leading, spacing: 6) {
                    Text(run.sender.name).font(Mac.sender)
                        .foregroundStyle(Color.madIdentity(id: run.sender.id, kind: run.sender.kind))
                        .padding(.leading, 3).lineLimit(1)
                    ForEach(run.bubbles) { b in bubble(b, mine: false, who: run.sender.name) }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.top, Mac.messageGap)
            #else
            // Grok's group chat: the sender's name in grey over the bubbles, the small avatar at the last bubble's bottom-left
            HStack(alignment: .bottom, spacing: Phone.chatSide - 6) {
                CharacterView(id: run.sender.id, kind: run.sender.kind, state: run.sender.state, size: Phone.chatAvatar)
                    .frame(width: Phone.chatAvatar, height: Phone.chatAvatar)
                    .onTapGesture { actions.openAgent(run.sender.id) }
                VStack(alignment: .leading, spacing: 4) {
                    Text(run.sender.name).font(Phone.sender).foregroundStyle(Color.madFg2).padding(.leading, 4).padding(.bottom, 2).lineLimit(1)
                    ForEach(run.bubbles) { b in bubble(b, mine: false, who: run.sender.name) }
                }
                // to the column's right edge (a Spacer here would add the HStack's spacing before it)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.top, 14)
            #endif
        }
    }

    private func bubble(_ b: ChannelBubble, mine: Bool, who: String) -> some View {
        ChannelBubbleView(
            bubble: b, mine: mine, maxWidth: columnWidth > 0 ? MadColumn.bubble(in: columnWidth) : 480, expanded: expanded.contains(b.id),
            toggle: { if expanded.contains(b.id) { expanded.remove(b.id) } else { expanded.insert(b.id) } },
            reply: {
                replying = ChannelQuote(seq: b.id, who: who, text: ChannelQuote.snippet(b.text.isEmpty ? L("[图片]") : b.text))
                actions.reply(b.id)
            },
            load: actions.loadPicture,
            openPicture: { viewing = $0 },
            colors: mentionColors)
    }

    /// Each member's name to its identity color: an @name takes it.
    private var mentionColors: [String: Color] {
        var out: [String: Color] = [:]
        for m in screen.panel.members + screen.panel.helpers where !m.dim {
            out[m.name] = Color.madIdentity(id: m.id, kind: m.kind)
        }
        return out
    }

    // MARK: the panel

    private var panelView: some View {
        #if os(iOS)
        ChannelPanelView(panel: screen.panel, actions: actions, openPictures: { gallery = true }, close: { panelOpen = false }, showsClose: true)
        #else
        ChannelPanelView(panel: screen.panel, actions: actions, openPictures: { gallery = true }, close: { panelOpen = false })
        #endif
    }

    private var galleryView: some View {
        let ids = screen.items.flatMap { item -> [String] in
            switch item {
            case .mine(let b): b.images
            case .run(let r): r.bubbles.flatMap(\.images)
            default: []
            }
        }
        return NavigationStack {
            ScrollView {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 100), spacing: 6)], spacing: 6) {
                    ForEach(Array(ids.enumerated()), id: \.offset) { _, id in
                        PictureThumb(id: id, size: 110, load: actions.loadPicture).onTapGesture { gallery = false; viewing = id }
                    }
                }
                .padding(12)
                if ids.isEmpty { Text(L("这个群里还没有图片。")).foregroundStyle(.secondary).padding(40) }
            }
            .navigationTitle(L("图片和文件"))
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button(L("完成")) { gallery = false } } }
        }
        #if os(macOS)
        .frame(minWidth: 420, minHeight: 360)
        #endif
    }

    #if os(iOS)
    /// On the phone the panel covers 84% of the width from the right, over a scrim; a swipe from the
    /// right edge opens it, a swipe back or a tap on the scrim closes it.
    private var phonePanel: some View {
        GeometryReader { geo in
            let width = min(geo.size.width * 0.84, 380)
            ZStack(alignment: .trailing) {
                if panelOpen || drag != 0 {
                    Color.black.opacity(0.45 * openFraction(width)).ignoresSafeArea()
                        .onTapGesture { withAnimation(.easeOut(duration: 0.25)) { panelOpen = false } }
                        .accessibilityHidden(true)
                }
                // built only while open or being dragged: a closed panel off screen would keep its
                // member rows and group tile animating unseen. Its slide in and out is the transition.
                if panelOpen || drag != 0 {
                panelView
                    .frame(width: width)
                    .offset(x: panelOffset(width))
                    .gesture(DragGesture(minimumDistance: 12).onChanged { v in
                        if panelOpen { drag = max(0, v.translation.width) }
                    }.onEnded { v in
                        withAnimation(.easeOut(duration: 0.25)) {
                            if panelOpen, v.translation.width > width * 0.3 { panelOpen = false }
                            drag = 0
                        }
                    })
                    .accessibilityHidden(!panelOpen)
                    .accessibilityAddTraits(.isModal)
                    .accessibilityAction(.escape) { panelOpen = false }
                    .transition(.move(edge: .trailing))
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .trailing)
        }
        .animation(.easeOut(duration: 0.25), value: panelOpen)
    }

    /// A swipe left that starts in the message area's last 22 points opens the panel. It is on the messages
    /// only (not the head or the composer, whose buttons it would swallow) and beside their scrolling.
    private var edgeOpen: some Gesture {
        DragGesture(minimumDistance: 10)
            .onChanged { v in
                guard !panelOpen, v.startLocation.x > chatWidth - 22, abs(v.translation.width) > abs(v.translation.height) else { return }
                drag = -min(panelWidth, max(0, -v.translation.width))
            }
            .onEnded { v in
                guard !panelOpen, drag != 0 else { return }
                withAnimation(.easeOut(duration: 0.25)) {
                    if -v.translation.width > panelWidth * 0.25 { panelOpen = true }
                    drag = 0
                }
            }
    }

    private var panelWidth: CGFloat { min(chatWidth * 0.84, 380) }

    private func panelOffset(_ width: CGFloat) -> CGFloat {
        if panelOpen { return drag }
        return width + (drag < 0 ? drag : 0)
    }

    private func openFraction(_ width: CGFloat) -> Double {
        if panelOpen { return 1 - Double(min(1, drag / width)) }
        return Double(min(1, -drag / width))
    }
    #endif
}


/// A row that is drawn again only when `item`, its open bubbles or the column's width changed (`.equatable()`):
/// SwiftUI does not run `content` for a row that compares equal.
private struct DrawnOnChange<Content: View>: View, Equatable {
    let item: ChannelItem
    let expanded: [Int]
    let width: CGFloat
    let content: () -> Content

    static func == (a: Self, b: Self) -> Bool { a.item == b.item && a.expanded == b.expanded && a.width == b.width }

    var body: some View { content() }
}

/// A count the view keeps without being drawn again for it.
#if os(macOS)
private final class RealizedRows { var count = 0; var markerFoot: CGFloat = .infinity }
#endif
