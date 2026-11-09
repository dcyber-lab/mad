import SwiftUI

#if os(iOS)
/// The phone's home, as the approved design (v6) draws it: a top bar with the person's picture, the Mac's name in a
/// capsule (it switches Mac) and +; what waits on the person pinned on top as an amber card that acts on the spot; the
/// group chats and each project (its branch and cost at the head's right) in Grok's rows; at the bottom a round grid
/// button (the menu sheet) and the search capsule.
public struct HomeView: View {
    let model: HomeModel
    let actions: HomeActions
    let menu: [[HomeMenuItem]]
    @State private var query = ""
    @State private var searching = false
    @State private var showMenu = false
    /// The item chosen in the menu sheet, carried out once the sheet is gone.
    @State private var chosen: HomeMenuItem?
    @FocusState private var searchFocused: Bool

    public init(model: HomeModel, actions: HomeActions = HomeActions(), menu: [[HomeMenuItem]] = HomeMenuItem.phone) {
        self.model = model
        self.actions = actions
        self.menu = menu
        #if DEBUG
        // Debug builds: `-nativeMenu YES` opens the menu sheet at once, to look at it without a tap.
        if UserDefaults.standard.bool(forKey: "nativeMenu") { _showMenu = State(initialValue: true) }
        #endif
    }

    public var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                if query.isEmpty && !model.waiting.isEmpty {
                    PhoneWaitingCard(items: model.waiting, actions: actions)
                        .padding(.horizontal, 16)
                        .padding(.top, 12)
                        .padding(.bottom, 6)
                }
                ForEach(model.filtered(by: query).sections) { section in
                    PhoneSectionHead(title: section.title, meta: section.meta)
                    ForEach(section.rows) { row in
                        Button { actions.open(row.id) } label: {
                            HomeRowView(row: row, metrics: .phone)
                                .padding(.horizontal, Phone.rowSide)
                                .frame(minHeight: Phone.rowHeight)
                                .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
            .padding(.bottom, 16)
        }
        .scrollDismissesKeyboard(.interactively)
        .background(Color.madPaper)
        .environment(\.madAvatarBackdrop, Color.madPaper)
        .safeAreaInset(edge: .top, spacing: 0) { topBar }
        .safeAreaInset(edge: .bottom, spacing: 0) { dock }
        .onChange(of: query) { _, q in actions.search(q) }
        .sheet(isPresented: $showMenu, onDismiss: {
            if let item = chosen { chosen = nil; actions.menu(item) }
        }) {
            PhoneMenuSheet(person: model.person ?? L("我"), machine: model.machine, groups: menu) { item in
                chosen = item
                showMenu = false
            }
            .presentationDetents([.height(470)])
            .presentationDragIndicator(.visible)
            .presentationCornerRadius(38)
            .presentationBackground(Color.madPaper)
        }
    }

    /// The person's picture, the Mac's name in a capsule, and +.
    private var topBar: some View {
        PhoneTopBar {
            Button { showMenu = true } label: {
                PersonPicture(name: model.person ?? L("我"), size: Phone.button)
            }
            .buttonStyle(.plain)
            .phoneFloat()
            .accessibilityLabel(model.person ?? L("我"))
        } center: {
            Button(action: actions.switchMac) {
                HStack(spacing: 6) {
                    Text(model.machine).font(Phone.title).foregroundStyle(Color.madFg).lineLimit(1).truncationMode(.tail)
                    Image(systemName: "chevron.down").font(.system(size: 12, weight: .bold)).foregroundStyle(Color.madFg3)
                }
                .padding(.horizontal, 18)
                .frame(height: Phone.button)
                .background(Color.phoneFloatFill, in: Capsule())
                .phoneFloat()
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(model.machine), switch Mac")
        } trailing: {
            Menu {
                ForEach(HomeNewItem.allCases, id: \.self) { item in
                    Button { actions.create(item) } label: { Label(item.title, systemImage: item.symbol) }
                }
            } label: {
                Image(systemName: "plus").font(.system(size: 19, weight: .medium)).foregroundStyle(Color.madFg)
                    .frame(width: Phone.button, height: Phone.button)
                    .background(Color.phoneFloatFill, in: Circle())
                    .contentShape(Circle())
            }
            .menuIndicator(.hidden)
            .phoneFloat()
            .accessibilityLabel("新建")
        }
    }

    /// The round grid button (the menu) and the search capsule; while searching the capsule is the field and the
    /// button is a way out of the search.
    private var dock: some View {
        HStack(spacing: 12) {
            Button {
                if searching { searching = false; query = ""; searchFocused = false } else { showMenu = true }
            } label: {
                Image(systemName: searching ? "xmark" : "square.grid.2x2").font(.system(size: 20, weight: .medium)).foregroundStyle(Color.madFg)
                    .frame(width: 50, height: 50)
                    .background(Color.phoneFloatFill, in: Circle())
                    .contentShape(Circle())
            }
            .buttonStyle(.plain)
            .phoneFloat()
            .accessibilityLabel(searching ? L("取消") : L("菜单"))
            HStack(spacing: 10) {
                Image(systemName: "magnifyingglass").font(.system(size: 19, weight: .medium)).foregroundStyle(Color.madFg)
                if searching {
                    TextField(L("搜索"), text: $query)
                        .textFieldStyle(.plain)
                        .font(.system(size: 18))
                        .focused($searchFocused)
                        .submitLabel(.search)
                } else {
                    Text(L("搜索")).font(.system(size: 18)).foregroundStyle(Color.madFg3)
                    Spacer(minLength: 0)
                }
            }
            .padding(.horizontal, 18)
            .frame(maxWidth: .infinity, minHeight: 50, maxHeight: 50)
            .background(Color.phoneFloatFill, in: Capsule())
            .contentShape(Capsule())
            .phoneFloat()
            .onTapGesture { searching = true; searchFocused = true }
            .accessibilityElement(children: .contain)
            .accessibilityLabel(L("搜索"))
        }
        .padding(.horizontal, Phone.rowSide)
        .padding(.top, 14)
        .padding(.bottom, Phone.footGap)
        .background(alignment: .bottom) {
            LinearGradient(stops: [.init(color: Color.madPaper.opacity(0), location: 0), .init(color: Color.madPaper, location: 0.45)],
                           startPoint: .top, endPoint: .bottom)
                .ignoresSafeArea(edges: .bottom)
                .allowsHitTesting(false)
        }
    }
}
#endif

/// The + menu: what the page's + starts. Each item is the app's to carry out (the page's dialog, for now).
struct HomeNewMenu: View {
    /// How the + is drawn: as a toolbar's glyph beside 🔍 and ⋯, as a round button, or as the small ＋ at the end of the
    /// sidebar's 群聊 head.
    enum Style { case toolbar, round, plain }

    let action: (HomeNewItem) -> Void
    var style = Style.toolbar

    var body: some View {
        Menu {
            ForEach(HomeNewItem.allCases, id: \.self) { item in
                Button { action(item) } label: { Label(item.title, systemImage: item.symbol) }
            }
        } label: {
            switch style {
            case .toolbar:
                Image(systemName: "plus").font(.system(size: 17, weight: .medium))
            case .round:
                // the whole ring is the button, as for hide and 🔍: the circle is in the label, not around the menu
                Image(systemName: "plus").font(.system(size: 14, weight: .medium)).foregroundStyle(.primary)
                    .frame(width: 30, height: 30)
                    .background(Color.chatComposer, in: Circle())
                    .overlay(Circle().strokeBorder(Color.chatLine.opacity(0.7)))
                    .contentShape(Circle())
            case .plain:
                Image(systemName: "plus").font(.system(size: 11, weight: .semibold)).foregroundStyle(Color.madFg3)
            }
        }
        .menuIndicator(.hidden)
        .accessibilityLabel("新建")
    }
}

/// The ⋯ menu: the items in groups, a divider between groups.
struct HomeMenu: View {
    let groups: [[HomeMenuItem]]
    let action: (HomeMenuItem) -> Void
    /// The glyph's font (the phone's toolbar draws it as it draws 🔍 and +).
    var font: Font?

    var body: some View {
        Menu {
            ForEach(Array(groups.enumerated()), id: \.offset) { i, group in
                if i > 0 { Divider() }
                ForEach(group, id: \.self) { item in Button(item.title) { action(item) } }
            }
        } label: {
            Image(systemName: "ellipsis").font(font)
        }
        .accessibilityLabel("更多")
    }
}
