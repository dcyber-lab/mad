import SwiftUI

#if os(iOS)
/// The phone's home list: the Mac's name with ▾, search, + and ⋯ above; the group chats first, then a
/// section per project; a row is a character, a name and the last line.
public struct HomeView: View {
    let model: HomeModel
    let actions: HomeActions
    let menu: [[HomeMenuItem]]
    @State private var query = ""
    @State private var searching = false

    public init(model: HomeModel, actions: HomeActions = HomeActions(), menu: [[HomeMenuItem]] = HomeMenuItem.phone) {
        self.model = model
        self.actions = actions
        self.menu = menu
    }

    public var body: some View {
        NavigationStack {
            List {
                ForEach(model.filtered(by: query).sections) { section in
                    Section {
                        ForEach(section.rows) { row in
                            Button { actions.open(row.id) } label: { HomeRowView(row: row, size: 46) }
                                .buttonStyle(.plain)
                                .listRowSeparator(.hidden)
                                .listRowInsets(EdgeInsets(top: 6, leading: 20, bottom: 6, trailing: 16))
                        }
                    } header: {
                        Text(section.title).font(.footnote).foregroundStyle(.secondary).textCase(nil)
                    }
                }
            }
            .listStyle(.plain)
            .searchable(text: $query, isPresented: $searching, placement: .navigationBarDrawer(displayMode: .automatic), prompt: "搜索")
            .onChange(of: query) { _, q in actions.search(q) }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button(action: actions.switchMac) {
                        HStack(spacing: 4) {
                            Text(model.machine).font(.title3.weight(.bold)).lineLimit(1).truncationMode(.tail)
                            Image(systemName: "chevron.down").font(.footnote).foregroundStyle(.secondary)
                        }
                        // the toolbar squeezes a long name to one letter; a width keeps it, ellipsized
                        .frame(width: min(CGFloat(model.machine.count) * 12 + 24, 168), alignment: .leading)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("\(model.machine), switch Mac")
                }
                ToolbarItemGroup(placement: .topBarTrailing) {
                    Button { searching = true } label: { Image(systemName: "magnifyingglass").font(.system(size: 17, weight: .medium)) }
                        .accessibilityLabel("搜索")
                    HomeNewMenu(action: actions.create)
                    HomeMenu(groups: menu, action: actions.menu, font: .system(size: 17, weight: .medium))
                }
            }
        }
    }
}
#endif

/// The + menu: what the page's + starts. Each item is the app's to carry out (the page's dialog, for now).
struct HomeNewMenu: View {
    /// How the + is drawn: as a toolbar's glyph beside 🔍 and ⋯, or as the Mac sidebar's round button.
    enum Style { case toolbar, round }

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
