#if os(macOS)
import AppKit
import SwiftUI

/// The Mac's sidebar, as design v6 draws it (docs/design/mac-v6.html): on the traffic lights' line two round floating
/// buttons (search, +), under them the Mac's name (it switches Mac); the 今日 row; what waits on the person as roomy
/// amber cards with Allow and Deny (Reply for a question); the group chats; then each project with its branch and
/// cost on its head line, and its agents. At the foot the person (a click opens Usage) and a round grid button
/// whose popover holds the menu (a right click on the person offers it too).
public struct MacSidebar: View {
    let model: HomeModel
    let actions: HomeActions
    let menu: [[HomeMenuItem]]
    let toggle: () -> Void
    @Binding var selection: HomeRow.ID?
    @State private var query = ""
    /// The search field is under the top while it is used (the round button or ⌘K opens it).
    @State private var searching = false
    @FocusState private var searchFocused: Bool
    /// The projects folded (HomeCollapsed), the filter's choice, and the row 删除 was chosen on until it is answered.
    @AppStorage(HomeCollapsed.key) private var collapsedRaw = "[]"
    @AppStorage("mad.home.hideStopped") private var hideStopped = false
    @State private var deleting: HomeRow?

    public init(model: HomeModel, selection: Binding<HomeRow.ID?>, actions: HomeActions = HomeActions(),
                menu: [[HomeMenuItem]] = HomeMenuItem.sidebar, toggle: @escaping () -> Void = {}) {
        self.model = model
        self.actions = actions
        self.menu = menu
        self.toggle = toggle
        _selection = selection
    }

    public var body: some View {
        VStack(spacing: 0) {
            top
            machine
            if searching { search }
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 4) {
                    if query.isEmpty { todayRow; waitingSection }
                    ForEach(model.filtered(by: query).sections) { section in
                        head(section)
                        if isOpen(section) {
                            ForEach(shown(section)) { row in
                                Button { selection = row.id; actions.open(row.id) } label: {
                                    SidebarRow(row: row, menu: actions.remove == nil || row.kind != .agent ? nil : { deleting = row })
                                        .padding(.horizontal, 10)
                                        .frame(height: Mac.rowHeight)
                                        .modifier(SelectedRowBackground(selected: selection == row.id))
                                }
                                .buttonStyle(.plain)
                            }
                        }
                    }
                }
                .padding(.horizontal, 10)
                .padding(.top, 4)
                .padding(.bottom, 14)
            }
            footer
        }
        .background(Color.madSide)
        .environment(\.madAvatarBackdrop, Color.madSide)
        .confirmationDialog(Lf("删除“%@”？", deleting?.title ?? ""), isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                            titleVisibility: .visible, presenting: deleting) { row in
            Button(L("删除"), role: .destructive) {
                if selection == row.id { selection = nil }
                actions.remove?(row.id)
            }
            Button(L("取消"), role: .cancel) {}
        } message: { _ in
            Text(L("它会停止，并从 mad 里移除。"))
        }
        .background {
            // ⌘K goes to the search field
            Button("") { openSearch() }.keyboardShortcut("k", modifiers: .command).opacity(0).frame(width: 0, height: 0)
                .accessibilityHidden(true)
        }
    }

    private func openSearch() {
        searching = true
        searchFocused = true
    }

    private func closeSearch() {
        query = ""
        searchFocused = false
        searching = false
    }

    /// The traffic lights' row: their room at the left (the window draws them), two round floating buttons at the right.
    private var top: some View {
        HStack(spacing: 8) {
            Spacer(minLength: 76)
            MacRoundButton { searching ? closeSearch() : openSearch() } label: { Image(systemName: "magnifyingglass") }
                .accessibilityLabel("搜索")
                .help(L("搜索或跳转"))
            MacNewMenu(action: actions.create)
        }
        .padding(.leading, 20)
        .padding(.trailing, 14)
        .frame(height: 52)
    }

    /// The Mac's name under the top: quiet, with a chevron; it switches Mac.
    private var machine: some View {
        Button(action: actions.switchMac) {
            HStack(spacing: 6) {
                Text(model.machine).lineLimit(1).truncationMode(.tail)
                Image(systemName: "chevron.down").font(.system(size: 11, weight: .semibold))
            }
            .font(.system(size: 14.5, weight: .medium))
            .foregroundStyle(Color.madFg2)
            .padding(.horizontal, 10)
            .padding(.vertical, 4)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 10)
        .padding(.bottom, 6)
        .accessibilityLabel("切换 Mac")
        .help(model.machine)
    }

    /// 搜索或跳转, with ⌘K at its end; it filters the list as it is typed, and Esc clears it and closes it.
    private var search: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass").font(.system(size: 13, weight: .medium))
            TextField(L("搜索或跳转"), text: $query)
                .textFieldStyle(.plain)
                .font(.system(size: 15))
                .foregroundStyle(Color.madFg)
                .focused($searchFocused)
                .onChange(of: query) { _, q in actions.search(q) }
                .onExitCommand { closeSearch() }
                .accessibilityLabel("搜索")
            if query.isEmpty { Kbd("⌘K") }
        }
        .foregroundStyle(Color.madFg3)
        .padding(.horizontal, 12)
        .frame(height: 36)
        .background(Color.macFloatFill, in: Capsule())
        .macFloat()
        .padding(.horizontal, 14)
        .padding(.bottom, 8)
    }

    private var todayRow: some View {
        Button { selection = nil; actions.open(HomeRow.todayID) } label: {
            TodayRowView(today: model.today, waiting: model.waiting.count)
                .padding(.horizontal, 10)
                .frame(height: Mac.rowHeight)
                .modifier(SelectedRowBackground(selected: selection == nil || selection == HomeRow.todayID))
        }
        .buttonStyle(.plain)
    }

    /// 待处理: what waits, oldest first, as cards; "都处理完了" when nothing does.
    @ViewBuilder private var waitingSection: some View {
        ListHead(title: L("待处理"), count: model.waiting.count, attention: true) { Kbd("⌥N") }
        VStack(spacing: 8) {
            ForEach(model.waiting) { WaitingCard(item: $0, actions: actions) }
            if model.waiting.isEmpty {
                Text(L("都处理完了")).font(.system(size: 14)).foregroundStyle(Color.madFg3)
                    .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, 10).padding(.vertical, 2)
            }
        }
    }

    @ViewBuilder private func head(_ section: HomeSection) -> some View {
        switch section.kind {
        case .chats:
            ListHead(title: section.title) {
                MacSmallNewMenu(action: actions.create)
            }
        case .project:
            ProjectHeader(title: section.title, open: isOpen(section), toggle: { toggle(section) }, hideStopped: $hideStopped,
                          create: actions.createIn.map { create in { item in create(item, section.id) } })
                .font(Mac.sectionHead)
                .padding(.horizontal, 10)
                .padding(.top, 18)
                .padding(.bottom, 6)
        }
    }

    /// A project's fold; a search shows what matched, folded or not.
    private func isOpen(_ section: HomeSection) -> Bool {
        guard case .project = section.kind else { return true }
        return !query.isEmpty || !HomeCollapsed.ids(collapsedRaw).contains(section.id)
    }

    private func toggle(_ section: HomeSection) {
        collapsedRaw = HomeCollapsed.setting(section.id, folded: isOpen(section), in: collapsedRaw)
    }

    /// The rows drawn: with the filter on, a stopped session is left out unless it is the one open.
    private func shown(_ section: HomeSection) -> [HomeRow] {
        section.rows.filter { !(hideStopped && $0.kind == .agent && $0.state == .stopped && $0.id != selection) }
    }

    /// The person: their picture and name (a click opens Usage, a right click offers the menu), and the round grid
    /// button at the right, whose popover is the menu.
    private var footer: some View {
        HStack(spacing: 8) {
            Button { selection = HomeRow.usageID; actions.open(HomeRow.usageID) } label: {
                HStack(spacing: 12) {
                    PersonPicture(name: model.person ?? NSFullUserName(), size: Mac.rowAvatar)
                    Text(model.person ?? NSFullUserName()).font(MadType.rowName).foregroundStyle(Color.madFg).lineLimit(1)
                    Spacer(minLength: 0)
                }
                .padding(.horizontal, 10)
                .frame(height: Mac.rowHeight)
                .modifier(SelectedRowBackground(selected: selection == HomeRow.usageID))
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .contextMenu {
                ForEach(Array(menu.enumerated()), id: \.offset) { i, group in
                    if i > 0 { Divider() }
                    ForEach(group, id: \.self) { item in Button(item.title) { actions.menu(item) } }
                }
            }
            MacFooterMenu(groups: menu, action: actions.menu)
        }
        .padding(.leading, 10)
        .padding(.trailing, 14)
        .padding(.top, 8)
        .padding(.bottom, 10)
    }
}
#endif
