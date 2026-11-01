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
    @State private var keeper = BottomKeeper()
    /// The marker's whereabouts and when the list last followed a message (see `BottomWatch`).
    @State private var watch = BottomWatch()
    /// How many rows the lazy list has made now (not state: counting must not draw the view again).
    @State private var realized = RealizedRows()
    @State private var expanded: Set<Int> = []
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
            .ignoresSafeArea(.container, edges: .top) // the name floats at the very top, as in the conversation
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

    private var chat: some View {
        VStack(spacing: 0) {
            #if os(iOS)
            head
            #endif
            messages
            // the composer has the message column's edges, exactly
            ChannelComposer(placeholder: screen.placeholder, readOnly: screen.readOnly, mentions: screen.panel.members + screen.panel.helpers,
                            replying: $replying, send: actions.send)
                .frame(maxWidth: MadColumn.maxWidth)
                .padding(.horizontal, MadColumn.side)
                .frame(maxWidth: .infinity)
        }
        .background(Color.clear)
    }

    /// The head: back (phone), the tile with the name and how many work, ⓘ. On the Mac it is the window's
    /// toolbar, the only bar above the channel; on the phone a row of the view.
    private var head: some View {
        HStack(spacing: 10) {
            if let back = actions.back {
                Button { back() } label: {
                    Image(systemName: "chevron.backward").font(.system(size: 19, weight: .semibold)).foregroundStyle(.secondary).frame(width: 26, height: 34)
                }
                .buttonStyle(.plain).accessibilityLabel("返回")
            }
            nameButton
            infoButton
        }
        .padding(.horizontal, 12)
        .padding(.top, 4)
        .padding(.bottom, 10)
    }

    private var nameButton: some View {
        Button { panelOpen = true } label: {
            HStack(spacing: 10) {
                GroupTile(members: screen.members.filter { !$0.dim }.map { ($0.id, $0.kind, $0.state) }, size: 32,
                          dot: (screen.members + screen.helpers).contains { $0.state == .needsYou }) // faces: the team's; the dot: everyone's
                VStack(alignment: .leading, spacing: 0) {
                    Text(screen.title).font(MadType.title).lineLimit(1)
                    PillSubline(text: screen.stateLine)
                }
                #if os(iOS)
                Spacer(minLength: 0)
                #endif
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(screen.title)，\(screen.stateLine)")
        .accessibilityHint("群信息")
    }

    #if os(macOS)
    /// The channel's name, floating at the top of the window as a small pill: its tile, name and how it stands.
    /// It opens 群信息.
    private var namePill: some View {
        // hugs its text, never passes the pane (less the side margins): a long name is cut at the end
        nameButton
            .padding(.horizontal, 14).padding(.vertical, 6)
            .background(.regularMaterial, in: Capsule())
            .overlay(Capsule().strokeBorder(Color.chatLine.opacity(0.6)))
            .shadow(color: .black.opacity(0.08), radius: 8, y: 2)
            .padding(.horizontal, MadColumn.side)
            .padding(.top, 12)
    }
    #endif

    private var infoButton: some View {
        Button { panelOpen.toggle() } label: {
            Image(systemName: "info").font(.system(size: 15, weight: .semibold)).foregroundStyle(.secondary)
                .frame(width: 34, height: 34).background(Color.chatCard, in: Circle())
        }
        .buttonStyle(.plain).accessibilityLabel("群信息")
    }

    private var messages: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    // Each row is drawn again only when what it shows changed: a look that brought nothing new (or one
                    // new message) leaves the other rows alone.
                    ForEach(screen.items.reversed()) { item in
                        DrawnOnChange(item: item, expanded: expandedBubbles(of: item), width: columnWidth) { row(item) }.equatable().id(item.id).scaleEffect(x: 1, y: -1)
                            .onAppear { realized.count += 1 }
                            .onDisappear { realized.count -= 1 }
                    }
                    // The last row of the lazy stack: it appears and disappears as the scroll brings it
                    // in and out of view, so a person who has scrolled up to read is not pulled down.
                    Color.clear.frame(height: 1).id(Self.bottom)
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
                }
                .frame(maxWidth: MadColumn.maxWidth)
                .background(GeometryReader { g in
                    Color.clear.onAppear { columnWidth = g.size.width }.onChange(of: g.size.width) { _, w in columnWidth = w }
                })
                .padding(.horizontal, MadColumn.side)
                .frame(maxWidth: .infinity)
                #if os(macOS)
                .padding(.top, 76) // under the floating name
                #else
                .padding(.top, 6)
                #endif
                .padding(.bottom, 10)
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
                    .buttonStyle(.plain).padding(.bottom, 10).accessibilityLabel("到最新")
                    .transition(.opacity)
                }
            }
            .background(GeometryReader { g in
                Color.clear.onAppear { viewport = g.size }.onChange(of: g.size) { _, size in
                    viewport = size
                    // a list at its bottom stays there as the viewport changes: put back at once, not after a wait
                    if nearBottom || keeper.resized() { stick(proxy) }
                    watch.resized()
                    settle(proxy)
                }
            })
            #if os(macOS)
            // the list fades out under the name, which floats over it
            .overlay(alignment: .top) {
                LinearGradient(colors: [Color.chatBackdrop, Color.chatBackdrop.opacity(0)], startPoint: .top, endPoint: .bottom)
                    .frame(height: 72).allowsHitTesting(false)
            }
            .overlay(alignment: .top) { namePill }
            #endif
            #if os(iOS)
            .background(GeometryReader { g in
                Color.clear.onAppear { chatWidth = g.size.width }.onChange(of: g.size.width) { _, w in chatWidth = w }
            })
            .simultaneousGesture(edgeOpen)
            #endif
            .scaleEffect(x: 1, y: -1) // PROTOTYPE: the list is upside down, its rows too: the bottom is the top of the scroll view
            .scrollDismissesKeyboard(.interactively)
            #if DEBUG && os(iOS)
            // a scripted run for comparing layouts: keyboard up, three lines, scrolls with it up, down, up again, a send
            .task {
                guard UserDefaults.standard.bool(forKey: "protoDrive") else { return }
                func post(_ name: String, _ object: Any? = nil) { NotificationCenter.default.post(name: Notification.Name(name), object: object) }
                func wait(_ s: Double) async { try? await Task.sleep(for: .seconds(s)) }
                await wait(4); post("protoFocus")
                await wait(3); post("protoText", "第一行\n第二行\n第三行")
                await wait(3)
                let first = screen.items.first?.id, mid = screen.items.dropFirst(screen.items.count / 2).first?.id
                if let first { withAnimation(.easeInOut(duration: 1.2)) { proxy.scrollTo(first, anchor: .bottom) } }
                await wait(2.5)
                if let mid { withAnimation(.easeInOut(duration: 1.2)) { proxy.scrollTo(mid, anchor: .center) } }
                await wait(2.5)
                withAnimation(.easeInOut(duration: 1.2)) { proxy.scrollTo(Self.bottom, anchor: .top) }
                await wait(2.5); post("protoBlur")
                await wait(3.5); post("protoFocus")
                await wait(3); post("protoText", "测试"); await wait(0.5); post("protoSend")
                await wait(4)
            }
            #endif
            .onChange(of: screen.tail) { _, _ in
                var mine = false
                if case .mine? = screen.items.last { mine = true }
                // No animation: a lazy list scrolled under an animation while its rows are measured draws
                // gaps, and the new message pushing the marker out would flash the way-down button.
                if nearBottom || mine { watch.followed(); stick(proxy) }
            }
        }
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

    /// Back to the bottom once the layout has settled (after a resize that moved it).
    private func stick(_ proxy: ScrollViewProxy) {
        DispatchQueue.main.async { proxy.scrollTo(Self.bottom, anchor: .bottom) }
    }

    @ViewBuilder private func row(_ item: ChannelItem) -> some View {
        switch item {
        case .time(_, let text):
            Text(text).font(MadType.label).foregroundStyle(.secondary).frame(maxWidth: .infinity).padding(.top, 14).padding(.bottom, 8)
        case .note(let id, _) where id == "quiet" || id == "empty":
            Color.clear.frame(height: 1) // drawn over the list area (see `emptyLine`)
        case .note(_, let text):
            Text(text).font(MadType.label).foregroundStyle(.secondary).multilineTextAlignment(.center).frame(maxWidth: .infinity).padding(.vertical, 8)
        case .mine(let b):
            // against the column's right edge, as wide as its text; under the latest one, the word for how far it got
            VStack(alignment: .trailing, spacing: 4) {
                HStack(spacing: 0) {
                    Spacer(minLength: MadColumn.gutter)
                    bubble(b, mine: true, who: "自己")
                }
                if b.lastOwn { ReceiptLine(receipt: b.receipt) }
            }
            .padding(.top, 6)
        case .run(let run):
            #if os(macOS)
            // As in a chat with one bot: no gutter; the sender's small character stands by its name above the bubbles
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    CharacterView(id: run.sender.id, kind: run.sender.kind, state: run.sender.state, size: 20)
                        .onTapGesture { actions.openAgent(run.sender.id) }
                    Text(run.sender.name).font(MadType.label).foregroundStyle(.secondary).lineLimit(1)
                }
                .padding(.leading, 2)
                ForEach(run.bubbles) { b in bubble(b, mine: false, who: run.sender.name) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.top, 12)
            #else
            HStack(alignment: .bottom, spacing: 8) {
                CharacterView(id: run.sender.id, kind: run.sender.kind, state: run.sender.state, size: 26)
                    .onTapGesture { actions.openAgent(run.sender.id) }
                    .padding(.bottom, 2)
                VStack(alignment: .leading, spacing: 3) {
                    Text(run.sender.name).font(MadType.label).foregroundStyle(.secondary).padding(.leading, 4).lineLimit(1)
                    ForEach(run.bubbles) { b in bubble(b, mine: false, who: run.sender.name) }
                }
                // to the column's right edge (a Spacer here would add the HStack's spacing before it)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.top, 10)
            #endif
        }
    }

    private func bubble(_ b: ChannelBubble, mine: Bool, who: String) -> some View {
        ChannelBubbleView(
            bubble: b, mine: mine, maxWidth: columnWidth > 0 ? MadColumn.bubble(in: columnWidth) : 480, expanded: expanded.contains(b.id),
            toggle: { if expanded.contains(b.id) { expanded.remove(b.id) } else { expanded.insert(b.id) } },
            reply: {
                replying = ChannelQuote(seq: b.id, who: who, text: ChannelQuote.snippet(b.text.isEmpty ? "[图片]" : b.text))
                actions.reply(b.id)
            },
            load: actions.loadPicture,
            openPicture: { viewing = $0 })
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
                if ids.isEmpty { Text("这个群里还没有图片。").foregroundStyle(.secondary).padding(40) }
            }
            .navigationTitle("图片和文件")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("完成") { gallery = false } } }
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
private final class RealizedRows { var count = 0; var markerFoot: CGFloat = .infinity }
