#if os(macOS)
import AppKit
import SwiftUI

/// The Mac's sidebar, as the approved design (v5) draws it: the window's traffic lights with the machine's name as a
/// small capsule beside them (it switches Mac); a search field (⌘K); the 今日 row; what waits on the person pinned
/// on top as compact cards with Allow and Deny (Reply for a question); the group chats; then each project with its
/// branch, changes, unpushed commits and cost on its head line, and its agents. At the foot only the person: their
/// picture, their name and 用量 ›, which opens the Usage screen (a right click offers the rest of the old ⋯ menu).
public struct MacSidebar: View {
    let model: HomeModel
    let actions: HomeActions
    let menu: [[HomeMenuItem]]
    let toggle: () -> Void
    @Binding var selection: HomeRow.ID?
    @State private var query = ""
    @FocusState private var searchFocused: Bool

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
            search
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 1) {
                    if query.isEmpty { todayRow; waitingSection }
                    ForEach(model.filtered(by: query).sections) { section in
                        head(section)
                        ForEach(section.rows) { row in
                            Button { selection = row.id; actions.open(row.id) } label: {
                                HomeRowView(row: row, metrics: .mac)
                                    .padding(.horizontal, 8).padding(.vertical, 6)
                                    .modifier(SelectedRowBackground(selected: selection == row.id))
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }
                .padding(.horizontal, 8)
                .padding(.top, 4)
                .padding(.bottom, 12)
            }
            footer
        }
        .background(Color.madSide)
        .environment(\.madAvatarBackdrop, Color.madSide)
        .background {
            // ⌘K goes to the search field
            Button("") { searchFocused = true }.keyboardShortcut("k", modifiers: .command).opacity(0).frame(width: 0, height: 0)
                .accessibilityHidden(true)
        }
    }

    /// The traffic lights' row: their room at the left (the window draws them), the machine's capsule at the right.
    private var top: some View {
        HStack(spacing: 8) {
            Spacer(minLength: 76)
            Button(action: actions.switchMac) {
                Text(model.machine + " ▾")
                    .font(.system(size: 12))
                    .foregroundStyle(Color.madFg2)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 2)
                    .background(Color.madRaise, in: Capsule())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("切换 Mac")
            .help(model.machine)
        }
        .padding(.horizontal, 14)
        .padding(.top, 14)
        .padding(.bottom, 6)
        .frame(height: 40)
    }

    /// 搜索或跳转, with ⌘K at its end; it filters the list as it is typed, and Esc clears it.
    private var search: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass").font(.system(size: 12, weight: .medium))
            TextField(L("搜索或跳转"), text: $query)
                .textFieldStyle(.plain)
                .font(.system(size: 13))
                .foregroundStyle(Color.madFg)
                .focused($searchFocused)
                .onChange(of: query) { _, q in actions.search(q) }
                .onExitCommand { query = ""; searchFocused = false }
                .accessibilityLabel("搜索")
            if query.isEmpty { Kbd("⌘K") }
        }
        .foregroundStyle(Color.madFg3)
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .background(Color.madRaise, in: RoundedRectangle(cornerRadius: 9, style: .continuous))
        .padding(.horizontal, 12)
        .padding(.top, 8)
        .padding(.bottom, 6)
    }

    private var todayRow: some View {
        Button { selection = nil; actions.open(HomeRow.todayID) } label: {
            TodayRowView(today: model.today, waiting: model.waiting.count)
                .padding(.horizontal, 8).padding(.vertical, 6)
                .modifier(SelectedRowBackground(selected: selection == nil || selection == HomeRow.todayID))
        }
        .buttonStyle(.plain)
        .padding(.top, 2)
    }

    /// 待处理: what waits, oldest first, as cards; "都处理完了" when nothing does.
    @ViewBuilder private var waitingSection: some View {
        ListHead(title: L("待处理"), count: model.waiting.count, attention: true) { Kbd("⌥N") }
        VStack(spacing: 6) {
            ForEach(model.waiting) { WaitingCard(item: $0, actions: actions) }
            if model.waiting.isEmpty {
                Text(L("都处理完了")).font(.system(size: 12.5)).foregroundStyle(Color.madFg3)
                    .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, 2).padding(.vertical, 2)
            }
        }
    }

    @ViewBuilder private func head(_ section: HomeSection) -> some View {
        switch section.kind {
        case .chats:
            ListHead(title: section.title) {
                HomeNewMenu(action: actions.create, style: .plain)
                    .menuStyle(.borderlessButton)
                    .menuIndicator(.hidden)
                    .fixedSize()
            }
        case .project:
            ListHead(title: section.title) { if let meta = section.meta { ProjectMetaLine(meta: meta) } }
        }
    }

    /// The person: their picture and name, 用量 › at the right. A click opens Usage; a right click offers the menu.
    private var footer: some View {
        VStack(spacing: 0) {
            Rectangle().fill(Color.madLine).frame(height: 1)
            Button { selection = HomeRow.usageID; actions.open(HomeRow.usageID) } label: {
                HStack(spacing: 10) {
                    PersonPicture(name: model.person ?? NSFullUserName(), size: 28)
                    Text(model.person ?? NSFullUserName()).font(.system(size: 13.5, weight: .medium)).foregroundStyle(Color.madFg).lineLimit(1)
                    Spacer(minLength: 0)
                    Text(L("用量 ›")).font(.system(size: 11)).foregroundStyle(Color.madFg3)
                }
                .padding(.horizontal, 8).padding(.vertical, 6)
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
            .padding(.horizontal, 8)
            .padding(.top, 6)
            .padding(.bottom, 8)
        }
    }
}
#endif
