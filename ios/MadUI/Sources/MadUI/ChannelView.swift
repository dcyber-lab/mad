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
    @State private var markerFoot: CGFloat = 0
    @State private var bottomInset: CGFloat = 0
    /// The list was at its end when it was last judged with the same viewport and inset: what a size change finds it as,
    /// whatever the change itself did to the marker (a marker pushed out by the keyboard is not a reader who scrolled up).
    @State private var endLatch = true
    @State private var judgedViewport: CGFloat = -1
    @State private var judgedInset: CGFloat = -1
    #endif
    #if os(macOS)
    /// How tall the create card floating over the composer is (0 for none).
    @State private var createHeight: CGFloat = 0
    // The Mac's window is resized by the person and keeps its list at the bottom through that (see `BottomWatch`).
    @State private var keeper = BottomKeeper()
    @State private var watch = BottomWatch()
    /// How many rows the lazy list has made now (not state: counting must not draw the view again).
    @State private var realized = RealizedRows()
    #endif
    @State private var expanded: Set<Int> = []
    /// Which of the screen's rows the list holds (see `HeldWindow`): the last rows at first, older ones as the person scrolls up.
    @State private var window = HeldWindow()
    /// When the window last took older rows: the geometry that asks for more needs a moment to see the rows it has just got.
    @State private var tookAt = Date.distantPast
    /// The scroll is at rest (no finger, no flick), and older rows were wanted while it was not.
    @State private var scrollIdle = true
    @State private var olderWanted = false
    @State private var top = TopWatch()
    @State private var place = PlaceMemory()
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
    private static let space = "channelList"

    public var body: some View {
        #if os(macOS)
        chat
            .environment(\.madOpenCreated, actions.openCreated)
            .ignoresSafeArea(.container, edges: .top) // the head bar starts at the window's top, as the conversation's
            .environment(\.madAvatarBackdrop, Color.madPaper)
            .inspector(isPresented: $panelOpen) { panelView.inspectorColumnWidth(min: 260, ideal: 300, max: 380) }
            .sheet(isPresented: $gallery) { galleryView }
            .sheet(item: Binding(get: { viewing.map(Picked.init) }, set: { viewing = $0?.id })) { p in
                PictureViewer(id: p.id, load: actions.loadPicture) { viewing = nil }
            }
        #else
        chat
            .environment(\.madOpenCreated, actions.openCreated)
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

    /// A bot's create request that came from this channel: the confirm card, above the composer.
    #if os(iOS)
    @ViewBuilder private var createCard: some View {
        if let create = screen.create {
            #if os(iOS)
            CreateSheet(card: create, act: { actions.act(create.id, $0, $1) })
                .id(create.id)
                .transition(.move(edge: .bottom).combined(with: .opacity))
            #endif
        }
    }
    #endif

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
            .safeAreaInset(edge: .bottom, spacing: 0) {
                VStack(spacing: 10) { createCard; composerRow }.background(alignment: .bottom) { PhoneFootFade() }
            }
            .background(Color.madPaper)
        #else
        // no title bar (design v6): the list scrolls under a fade, the capsule and the round button float over it, and the
        // composer floats over the list's foot
        VStack(spacing: 0) {
            messages
            composerRow.macFootBar()
                // the card floats over the composer's top, as the conversation's ask does; the list keeps clear of it
                .overlay(alignment: .top) {
                    if let create = screen.create {
                        CreateCardView(card: create, act: { actions.act(create.id, $0, $1) })
                            .id(create.id)
                            .frame(maxWidth: MadColumn.maxWidth)
                            .padding(.horizontal, MadColumn.side)
                            .frame(maxWidth: .infinity)
                            .background(GeometryReader { g in
                                Color.clear
                                    .onAppear { createHeight = g.size.height }
                                    .onChange(of: g.size.height) { _, h in createHeight = h }
                                    .onDisappear { createHeight = 0 }
                            })
                            .offset(y: -(createHeight + 12))
                    }
                }
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
                    // The top of what is held, for the scroll to ask for older rows when it comes near. Its frame is read, not
                    // its appearing: a plain stack makes every row at once, so nothing appears or leaves as the scroll moves.
                    Color.clear.frame(height: PlaceMemory.sentinel)
                        .accessibilityIdentifier("channel.heldTop")
                        .background(GeometryReader { g in
                            Color.clear.onAppear { nearTop(g.frame(in: .named(Self.space)).minY, proxy) }
                                .onChange(of: g.frame(in: .named(Self.space)).minY) { _, y in nearTop(y, proxy) }
                        })
                    // Before iOS 18 nothing loads by itself (no way to tell a rest, no way to hold the place): a chip does.
                    if #unavailable(iOS 18, macOS 15), !window.holdsAll(screen.items.map(\.id)) {
                        EarlierChip { takeOlder(proxy, tapped: true) }
                    }
                    ForEach(held) { item in
                        DrawnOnChange(item: item, expanded: expandedBubbles(of: item), width: columnWidth) { row(item) }.equatable().id(item.id)
                            .background { if place.anchorID == item.id { anchorReader(item.id) } }
                            .background { if #unavailable(iOS 18, macOS 15), item.id == held.first?.id { firstRowReader() } }
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
                            Color.clear.onAppear { markerFoot = g.frame(in: .named(Self.space)).maxY; judge() }
                                .onChange(of: g.frame(in: .named(Self.space)).maxY) { _, y in markerFoot = y; judge() }
                        })
                        #else
                        .background(GeometryReader { g in
                            // where the marker's foot is in the scroll view, kept without drawing the view again
                            // A plain stack makes the marker once and keeps it: whether it is in view is its frame's, read here.
                            Color.clear.onAppear { markerMoved(g.frame(in: .named(Self.space)).maxY, proxy) }
                                .onChange(of: g.frame(in: .named(Self.space)).maxY) { _, y in markerMoved(y, proxy) }
                        })
                        #endif
                }
                .coordinateSpace(name: PlaceMemory.space)
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
            .keepsReadingPlace(place, idle: $scrollIdle)
            .onChange(of: scrollIdle) { _, idle in if idle && olderWanted { takeOlder(proxy) } }
            .scrollDismissesKeyboard(.interactively)
            #if os(macOS)
            .safeAreaInset(edge: .bottom, spacing: 0) { Color.clear.frame(height: createHeight > 0 ? createHeight + 12 : 0) }
            #endif
            .onAppear { pinWindow(proxy) }
            .onChange(of: screen.items.count) { _, _ in pinWindow(proxy) }
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

    private var held: ArraySlice<ChannelItem> { window.items(screen.items) }

    /// The top of the held rows is `y` (in the list's own space; 0 is the viewport's top): within five screens above it (a flick
    /// of the finger goes 1500 to 3000 points), the next older rows are held, once the scroll is at rest. The rows go in above
    /// what is read, which the list keeps where it is (`KeepsReadingPlace`). Before iOS 18 this does nothing: the chip does.
    private func nearTop(_ y: CGFloat, _ proxy: ScrollViewProxy) {
        top.y = y
        guard #available(iOS 18, macOS 15, *), wantsOlder else { return }
        olderWanted = true
        if scrollIdle { takeOlder(proxy) }
    }

    private var wantsOlder: Bool {
        canTakeOlder && viewport.height > 0 && top.y > -viewport.height * HeldWindow.lookAhead
    }

    private var canTakeOlder: Bool { !window.holdsAll(screen.items.map(\.id)) && Date().timeIntervalSince(tookAt) > 0.3 }

    private func takeOlder(_ proxy: ScrollViewProxy, tapped: Bool = false) {
        olderWanted = false
        guard tapped ? canTakeOlder : wantsOlder else { return }
        tookAt = Date()
        let readingAt = held.first?.id
        place.expectRows(above: readingAt, atEnd: nearBottom)
        let rowY = top.firstY, rowHeight = top.firstHeight
        window.earlier(in: screen.items.map(\.id))
        // The chip: the old first row goes back where it was in the viewport. A scroll view puts the point of the row at the
        // anchor's fraction of its own height at the same fraction of the viewport: fraction u of a row `h` tall at `u * (H - h)`.
        if tapped, let readingAt {
            let room = viewport.height - rowHeight
            let u = room > 1 ? min(max(rowY / room, 0), 1) : 0
            DispatchQueue.main.async { proxy.scrollTo(readingAt, anchor: UnitPoint(x: 0, y: u)) }
        }
        // the load made rows: ask again when the throttle is over, so short rows chain to the look-ahead while at rest
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.35) { nearTop(top.y, proxy) }
    }

    /// The first held row's place, for the chip's tap (before iOS 18 nothing else holds the reading place).
    private func firstRowReader() -> some View {
        GeometryReader { g in
            Color.clear.onAppear { firstRowMoved(g) }
                .onChange(of: g.frame(in: .named(Self.space)).minY) { _, _ in firstRowMoved(g) }
        }
    }

    private func firstRowMoved(_ g: GeometryProxy) {
        let f = g.frame(in: .named(Self.space))
        top.firstY = f.minY
        top.firstHeight = f.height
    }

    /// Reads the top of the row that was the first, in the stack's space, once the older rows are in above it.
    private func anchorReader(_ id: String) -> some View {
        GeometryReader { g in
            Color.clear.onAppear { place.anchorMeasured(id, top: g.frame(in: .named(PlaceMemory.space)).minY) }
                .onChange(of: g.frame(in: .named(PlaceMemory.space)).minY) { _, y in place.anchorMeasured(id, top: y) }
        }
    }

    /// The window names its first row once it is first shown, and again if that row is gone.
    /// A reader at the end whose held top is far above gets the stack cut back, and is put at the end again after it.
    private func pinWindow(_ proxy: ScrollViewProxy) {
        let far = nearBottom && viewport.height > 0 && HeldWindow.look(topY: top.y, viewport: viewport.height) == .trim
        if window.pin(in: screen.items.map(\.id), trim: far) { stick(proxy) }
    }

    #if os(macOS)
    /// The marker's foot is at `y`: in view or not. Out of view for a person who scrolled; a message that arrives pushes it
    /// out for a moment and the list follows it, which is judged when the follow is over (see `BottomWatch`).
    private func markerMoved(_ y: CGFloat, _ proxy: ScrollViewProxy) {
        realized.markerFoot = y
        guard viewport.height > 0 else { return }
        if y <= viewport.height + 2 {
            if !watch.shown || !nearBottom { watch.appeared(); nearBottom = true }
        } else if watch.shown {
            switch watch.disappeared() {
            case .now: left(proxy)
            case .recheck(let after):
                DispatchQueue.main.asyncAfter(deadline: .now() + after + 0.02) { if watch.stillLeft { left(proxy) } }
            }
        }
    }
    #endif

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
        nearBottom = markerFoot <= viewport.height + 2
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
        case .create(let line):
            CreateLineView(line: line, open: actions.openCreated)
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
