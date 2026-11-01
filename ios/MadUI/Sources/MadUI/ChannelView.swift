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
    @State private var expanded: Set<Int> = []
    @State private var replying: ChannelQuote?
    @State private var viewing: String?
    @State private var drag: CGFloat = 0
    /// The message area's width: the right-edge swipe starts in its last 22 points.
    @State private var chatWidth: CGFloat = 0

    /// panelOpen: whether the 群信息 panel is open when the view first shows.
    public init(screen: ChannelScreen, actions: ChannelActions, panelOpen: Bool = false) {
        self.screen = screen
        self.actions = actions
        _panelOpen = State(initialValue: panelOpen)
    }

    /// The message column is never wider than this on a wide window.
    private static let column: CGFloat = 720
    private static let bottom = "bottom"

    public var body: some View {
        #if os(macOS)
        chat
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
            head
            messages
            ChannelComposer(placeholder: screen.placeholder, readOnly: screen.readOnly, replying: $replying, send: actions.send)
                .frame(maxWidth: Self.column)
                .frame(maxWidth: .infinity)
        }
        .background(Color.clear)
    }

    private var head: some View {
        HStack(spacing: 10) {
            if let back = actions.back {
                Button { back() } label: {
                    Image(systemName: "chevron.backward").font(.system(size: 19, weight: .semibold)).foregroundStyle(.secondary).frame(width: 26, height: 34)
                }
                .buttonStyle(.plain).accessibilityLabel("返回")
            }
            Button { panelOpen = true } label: {
                HStack(spacing: 10) {
                    GroupTile(members: screen.members.filter { !$0.dim }.map { ($0.id, $0.kind, $0.state) }, size: 32,
                              dot: (screen.members + screen.helpers).contains { $0.state == .needsYou }) // faces: the team's; the dot: everyone's
                    VStack(alignment: .leading, spacing: 0) {
                        Text(screen.title).font(.headline).lineLimit(1)
                        if !screen.stateLine.isEmpty { Text(screen.stateLine).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                    }
                    Spacer(minLength: 0)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(screen.title)，\(screen.stateLine)")
            .accessibilityHint("群信息")
            Button { panelOpen.toggle() } label: {
                Image(systemName: "info").font(.system(size: 15, weight: .semibold)).foregroundStyle(.secondary)
                    .frame(width: 34, height: 34).background(Color.chatCard, in: Circle())
            }
            .buttonStyle(.plain).accessibilityLabel("群信息")
        }
        .padding(.horizontal, 12)
        .padding(.top, 4)
        .padding(.bottom, 10)
    }

    private var messages: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(screen.items) { item in row(item).id(item.id) }
                    // The last row of the lazy stack: it appears and disappears as the scroll brings it
                    // in and out of view, so a person who has scrolled up to read is not pulled down.
                    Color.clear.frame(height: 1).id(Self.bottom)
                        .onAppear { nearBottom = true }
                        .onDisappear { nearBottom = false }
                }
                .frame(maxWidth: Self.column)
                .frame(maxWidth: .infinity)
                .padding(.horizontal, 14)
                .padding(.top, 6)
                .padding(.bottom, 10)
            }
            #if os(iOS)
            .background(GeometryReader { g in
                Color.clear.onAppear { chatWidth = g.size.width }.onChange(of: g.size.width) { _, w in chatWidth = w }
            })
            .simultaneousGesture(edgeOpen)
            #endif
            .defaultScrollAnchor(.bottom)
            .scrollDismissesKeyboard(.interactively)
            .onChange(of: screen.tail) { _, _ in
                var mine = false
                if case .mine? = screen.items.last { mine = true }
                if nearBottom || mine { withAnimation { proxy.scrollTo(Self.bottom, anchor: .bottom) } }
            }
        }
    }

    @ViewBuilder private func row(_ item: ChannelItem) -> some View {
        switch item {
        case .time(_, let text):
            Text(text).font(.caption).foregroundStyle(.secondary).frame(maxWidth: .infinity).padding(.top, 14).padding(.bottom, 8)
        case .note(_, let text):
            Text(text).font(.caption).foregroundStyle(.secondary).multilineTextAlignment(.center).frame(maxWidth: .infinity).padding(.vertical, 8)
        case .mine(let b):
            HStack(spacing: 0) {
                Spacer(minLength: 56)
                bubble(b, mine: true, who: "自己")
            }
            .padding(.top, 6)
        case .run(let run):
            HStack(alignment: .bottom, spacing: 8) {
                CharacterView(id: run.sender.id, kind: run.sender.kind, state: run.sender.state, size: 26)
                    .onTapGesture { actions.openAgent(run.sender.id) }
                    .padding(.bottom, 2)
                VStack(alignment: .leading, spacing: 3) {
                    Text(run.sender.name).font(.caption).foregroundStyle(.secondary).padding(.leading, 4).lineLimit(1)
                    ForEach(run.bubbles) { b in bubble(b, mine: false, who: run.sender.name) }
                }
                Spacer(minLength: 28)
            }
            .padding(.top, 10)
        }
    }

    private func bubble(_ b: ChannelBubble, mine: Bool, who: String) -> some View {
        ChannelBubbleView(
            bubble: b, mine: mine, expanded: expanded.contains(b.id),
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
