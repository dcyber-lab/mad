import SwiftUI

/// The same list as a sidebar, for a NavigationSplitView: the same model, 40pt characters, a selection,
/// keyboard navigation from List, and a one-line usage footer.
public struct HomeSidebar: View {
    let model: HomeModel
    let actions: HomeActions
    let menu: [[HomeMenuItem]]
    @Binding var selection: HomeRow.ID?
    @State private var query = ""
    @State private var searching = false
    @FocusState private var searchFocused: Bool

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
                TextField("搜索", text: $query)
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
                    Section {
                        ForEach(section.rows) { row in
                            HomeRowView(row: row, size: 40)
                                .tag(row.id)
                                .listRowSeparator(.hidden)
                        }
                    } header: {
                        Text(section.title).font(MadType.label).foregroundStyle(.secondary)
                    }
                }
            }
            .listStyle(.sidebar)
            .onChange(of: selection) { _, id in if let id { actions.open(id) } }
            if let usage = model.usage {
                Text(usage)
                    .font(MadType.label)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 8)
            }
        }
        #if os(macOS)
        .ignoresSafeArea(.container, edges: .top) // the header takes the title bar's band
        #endif
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
            Button(action: actions.newChat) { Image(systemName: "plus") }
                .accessibilityLabel("新建")
            HomeMenu(groups: menu, action: actions.menu)
                #if os(macOS)
                .menuStyle(.borderlessButton)
                .menuIndicator(.hidden)
                .fixedSize()
                #endif
        }
    }
}
