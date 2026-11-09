import SwiftUI

#if os(iOS)
/// The phone's home, as the approved design (v5) draws it: "mad" with search, + and ⋯ beside it and the Mac's name as
/// a small capsule under it (it switches Mac); what waits on the person pinned on top as cards that act on the spot;
/// the group chats; each project with its git and cost on its head line, and its agents. At the foot the person and
/// 用量 ›, which opens the Usage screen. No quota bars here: usage is its own screen.
public struct HomeView: View {
    let model: HomeModel
    let actions: HomeActions
    let menu: [[HomeMenuItem]]
    @State private var query = ""
    @State private var searching = false
    @FocusState private var searchFocused: Bool

    public init(model: HomeModel, actions: HomeActions = HomeActions(), menu: [[HomeMenuItem]] = HomeMenuItem.phone) {
        self.model = model
        self.actions = actions
        self.menu = menu
    }

    public var body: some View {
        List {
            if query.isEmpty { waitingSection }
            ForEach(model.filtered(by: query).sections) { section in
                Section {
                    ForEach(section.rows) { row in
                        Button { actions.open(row.id) } label: { HomeRowView(row: row, metrics: .phone) }
                            .buttonStyle(.plain)
                            .listRowSeparator(.hidden)
                            .listRowInsets(EdgeInsets(top: 8, leading: 18, bottom: 8, trailing: 16))
                    }
                } header: {
                    head(section).textCase(nil).listRowInsets(EdgeInsets(top: 0, leading: 10, bottom: 0, trailing: 16))
                }
            }
        }
        .listStyle(.plain)
        .environment(\.madAvatarBackdrop, Color.madPaper)
        .safeAreaInset(edge: .top, spacing: 0) { header.background(.bar) }
        .safeAreaInset(edge: .bottom, spacing: 0) { footer.background(.bar) }
        .onChange(of: query) { _, q in actions.search(q) }
    }

    /// "mad", the round search, + and ⋯; the Mac's capsule under it; the search field while searching.
    private var header: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 10) {
                Text("mad").font(.system(size: 30, weight: .bold)).tracking(-0.6).foregroundStyle(Color.madFg)
                Spacer(minLength: 0)
                round {
                    Button {
                        searching.toggle()
                        if searching { searchFocused = true } else { query = "" }
                    } label: { Image(systemName: "magnifyingglass") }
                        .accessibilityLabel("搜索")
                }
                round { HomeNewMenu(action: actions.create) }
                round { HomeMenu(groups: menu, action: actions.menu) }
            }
            Button(action: actions.switchMac) {
                Text(model.machine + " ▾")
                    .font(.system(size: 12.5))
                    .foregroundStyle(Color.madFg2)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 2)
                    .background(Color.madRaise, in: Capsule())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(model.machine), switch Mac")
            if searching {
                TextField(L("搜索"), text: $query)
                    .textFieldStyle(.plain)
                    .focused($searchFocused)
                    .padding(.horizontal, 12).padding(.vertical, 8)
                    .background(Color.madRaise, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                    .submitLabel(.search)
            }
        }
        .padding(.horizontal, 18)
        .padding(.top, 6)
        .padding(.bottom, 10)
    }

    private func round<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        content()
            .font(.system(size: 15, weight: .medium))
            .foregroundStyle(Color.madFg)
            .buttonStyle(.plain)
            .frame(width: 34, height: 34)
            .background(Color.madRaise, in: Circle())
    }

    @ViewBuilder private var waitingSection: some View {
        Section {
            VStack(spacing: 8) {
                ForEach(model.waiting) { WaitingCard(item: $0, actions: actions, phone: true) }
                if model.waiting.isEmpty {
                    Text(L("都处理完了")).font(.footnote).foregroundStyle(Color.madFg3).frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            .listRowSeparator(.hidden)
            .listRowInsets(EdgeInsets(top: 2, leading: 18, bottom: 6, trailing: 18))
        } header: {
            ListHead(title: L("待处理"), count: model.waiting.count, attention: true, phone: true) { EmptyView() }
                .textCase(nil)
                .listRowInsets(EdgeInsets(top: 0, leading: 10, bottom: 0, trailing: 16))
        }
    }

    @ViewBuilder private func head(_ section: HomeSection) -> some View {
        if let meta = section.meta {
            ListHead(title: section.title, phone: true) { ProjectMetaLine(meta: meta) }
        } else {
            ListHead(title: section.title, phone: true) { EmptyView() }
        }
    }

    /// The person and 用量 ›: the way to the Usage screen.
    private var footer: some View {
        VStack(spacing: 0) {
            Divider()
            Button { actions.open(HomeRow.usageID) } label: {
                HStack(spacing: 10) {
                    PersonPicture(name: model.person ?? L("我"), size: 28)
                    Text(model.person ?? L("我")).font(.system(size: 15, weight: .medium)).foregroundStyle(Color.madFg)
                    Spacer(minLength: 0)
                    Text(L("用量 ›")).font(.system(size: 13)).foregroundStyle(Color.madFg3)
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 8)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
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
