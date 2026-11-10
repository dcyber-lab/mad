import SwiftUI

/// The same list as a sidebar, for a NavigationSplitView: the same model and rows as the Mac's own sidebar, a
/// selection and keyboard navigation from List. No usage footer: usage is its own screen.
public struct HomeSidebar: View {
    let model: HomeModel
    let actions: HomeActions
    let menu: [[HomeMenuItem]]
    @Binding var selection: HomeRow.ID?
    @State private var query = ""
    @State private var searching = false
    @FocusState private var searchFocused: Bool
    @AppStorage(HomeCollapsed.key) private var collapsedRaw = "[]"
    /// The agent row 删除 was chosen on, until the person confirms or cancels.
    @State private var deleting: HomeRow?

    public init(model: HomeModel, selection: Binding<HomeRow.ID?>, actions: HomeActions = HomeActions(),
                menu: [[HomeMenuItem]] = HomeMenuItem.sidebar) {
        self.model = model
        self.actions = actions
        self.menu = menu
        _selection = selection
    }

    public var body: some View {
        VStack(spacing: 0) {
            header
            if searching {
                TextField(L("搜索"), text: $query)
                    .textFieldStyle(.roundedBorder)
                    .focused($searchFocused)
                    .padding(.horizontal, 12)
                    .padding(.bottom, 6)
                    .onChange(of: query) { _, q in actions.search(q) }
                    #if os(macOS)
                    .onExitCommand { query = ""; searching = false }
                    #endif
            }
            List(selection: $selection) {
                ForEach(model.filtered(by: query).sections) { section in
                    if case .project = section.kind {
                        Section(isExpanded: expanded(section)) {
                            rows(section)
                        } header: {
                            ProjectHeader(title: section.title, create: actions.createIn.map { create in
                                { item in create(item, section.id) }
                            })
                        }
                    } else {
                        Section {
                            rows(section)
                        } header: {
                            Text(section.title).font(MadType.label).foregroundStyle(.secondary)
                        }
                    }
                }
            }
            .listStyle(.sidebar)
            .onChange(of: selection) { _, id in if let id { actions.open(id) } }
            .confirmationDialog(Lf("删除“%@”？", deleting?.title ?? ""), isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                                titleVisibility: .visible, presenting: deleting) { row in
                Button(L("删除"), role: .destructive) { actions.remove?(row.id) }
                Button(L("取消"), role: .cancel) {}
            } message: { _ in
                Text(L("它会停止，并从 mad 里移除。"))
            }
        }
        #if os(macOS)
        .ignoresSafeArea(.container, edges: .top) // the header takes the title bar's band
        #endif
    }

    private func rows(_ section: HomeSection) -> some View {
        ForEach(section.rows) { row in
            HomeRowView(row: row, metrics: .mac)
                .tag(row.id)
                .listRowSeparator(.hidden)
                .contextMenu {
                    if row.kind == .agent, actions.remove != nil {
                        Button(role: .destructive) { deleting = row } label: { Label(L("删除"), systemImage: "trash") }
                    }
                }
        }
    }

    /// A project's fold. A search shows what matched, folded or not.
    private func expanded(_ section: HomeSection) -> Binding<Bool> {
        Binding(
            get: { !query.isEmpty || !HomeCollapsed.ids(collapsedRaw).contains(section.id) },
            set: { collapsedRaw = HomeCollapsed.setting(section.id, folded: !$0, in: collapsedRaw) })
    }

    private var header: some View {
        HStack(spacing: 8) {
            Text(model.machine).font(MadType.title).lineLimit(1).frame(maxWidth: .infinity, alignment: .leading)
            headerButtons
        }
        .buttonStyle(.borderless)
        #if os(macOS)
        // The name and the buttons sit in the title bar's own band, beside the traffic lights and the
        // sidebar toggle, so there is one row there and not two.
        .padding(.leading, 84)
        .padding(.trailing, 44)
        .padding(.top, 8)
        .frame(height: 48)
        #else
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        #endif
    }

    @ViewBuilder private var headerButtons: some View {
        Group {
            Button {
                searching.toggle()
                if searching { searchFocused = true } else { query = "" }
            } label: { Image(systemName: "magnifyingglass") }
                .accessibilityLabel("搜索")
            HomeNewMenu(action: actions.create)
                #if os(macOS)
                .menuStyle(.borderlessButton)
                .menuIndicator(.hidden)
                .fixedSize()
                #endif
            HomeMenu(groups: menu, action: actions.menu)
                #if os(macOS)
                .menuStyle(.borderlessButton)
                .menuIndicator(.hidden)
                .fixedSize()
                #endif
        }
    }
}

/// A project's header: its name, and a + at the right (on the Mac it shows when the pointer is over the header) that
/// offers 新对话, 新群聊 and 新团队 for it.
private struct ProjectHeader: View {
    let title: String
    let create: ((HomeNewItem) -> Void)?
    @State private var hovering = false

    var body: some View {
        HStack(spacing: 4) {
            Text(title).font(MadType.label).foregroundStyle(.secondary).lineLimit(1)
            Spacer(minLength: 0)
            if let create {
                Menu {
                    ForEach(HomeNewItem.allCases, id: \.self) { item in
                        Button { create(item) } label: { Label(item.title, systemImage: item.symbol) }
                    }
                } label: {
                    Image(systemName: "plus").font(.system(size: 12, weight: .medium))
                }
                .menuStyle(.borderlessButton)
                .menuIndicator(.hidden)
                .fixedSize()
                .opacity(hovering ? 1 : 0)
                .accessibilityLabel(L("在这个项目里新建"))
            }
        }
        .contentShape(Rectangle())
        .onHover { hovering = $0 }
    }
}
