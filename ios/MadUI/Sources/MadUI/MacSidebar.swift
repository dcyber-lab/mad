#if os(macOS)
import SwiftUI

/// The Mac's sidebar, as a chat app draws it: the window's controls over a band with round buttons at the right
/// (hide the sidebar, search, new), the machine as a big tile with its name under it, the list with a soft grey
/// row for the open one, and the usage and the ⋯ menu at the bottom. It sits on a surface of its own and has no
/// system chrome.
public struct MacSidebar: View {
    let model: HomeModel
    let actions: HomeActions
    let menu: [[HomeMenuItem]]
    let toggle: () -> Void
    @Binding var selection: HomeRow.ID?
    @State private var query = ""
    @State private var searching = false
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
            band
            if searching {
                TextField("搜索", text: $query)
                    .textFieldStyle(.plain)
                    .font(MadType.rowLine)
                    .focused($searchFocused)
                    .padding(.horizontal, 12).padding(.vertical, 8)
                    .background(Color.chatComposer, in: Capsule())
                    .overlay(Capsule().strokeBorder(Color.chatLine))
                    .padding(.horizontal, 14).padding(.bottom, 8)
                    .onChange(of: query) { _, q in actions.search(q) }
                    .onExitCommand { query = ""; searching = false }
            }
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 2) {
                    if !searching { identity }
                    ForEach(model.filtered(by: query).sections) { section in
                        Text(section.title).font(MadType.label).foregroundStyle(.secondary)
                            .padding(.horizontal, 20).padding(.top, 14).padding(.bottom, 4)
                        ForEach(section.rows) { row in
                            Button { selection = row.id; actions.open(row.id) } label: {
                                HomeRowView(row: row, size: 40)
                                    .padding(.horizontal, 10).padding(.vertical, 7)
                                    .background(selection == row.id ? Color.primary.opacity(0.08) : Color.clear,
                                                in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                            }
                            .buttonStyle(.plain)
                            .padding(.horizontal, 8)
                        }
                    }
                }
                .padding(.bottom, 12)
            }
            footer
        }
        .background(Color.chatSidebar)
    }

    /// The row the window's controls are on: round buttons at the right on the traffic lights' line (their centre is
    /// 16pt below the top), the traffic lights' room at the left.
    private var band: some View {
        HStack(spacing: 10) {
            Spacer(minLength: 0)
            round("sidebar.left", "收起侧栏", toggle)
            round("magnifyingglass", "搜索") {
                searching.toggle()
                if searching { searchFocused = true } else { query = "" }
            }
            HomeNewMenu(action: actions.create, style: .round)
                .menuStyle(.button)
                .buttonStyle(.plain)
                .fixedSize()
        }
        .padding(.horizontal, 14)
        .frame(height: 32)
    }

    private func round(_ symbol: String, _ label: String, _ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol).font(.system(size: 14, weight: .medium)).foregroundStyle(.primary)
                .frame(width: 30, height: 30)
                .background(Color.chatComposer, in: Circle())
                .overlay(Circle().strokeBorder(Color.chatLine.opacity(0.7)))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
    }

    /// The machine: its agents' characters in one big tile, and its name.
    private var identity: some View {
        VStack(spacing: 8) {
            GroupTile(members: faces, size: 72, dot: false)
            // up to two lines, centred; cut only past that
            Text(model.machine).font(MadType.label.weight(.semibold)).lineLimit(2).multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true).padding(.horizontal, 18)
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 20).padding(.bottom, 10)
    }

    private var faces: [GroupTile.Member] {
        let agents = model.sections.flatMap(\.rows).filter { $0.kind == .agent }
        return agents.prefix(3).map { (id: $0.id, kind: $0.agentKind, state: $0.state) }
    }

    private var footer: some View {
        HStack(spacing: 10) {
            if let usage = model.usage {
                // "Claude 5 小时 42% · 本周 71%": a click opens 用量
                Button { actions.menu(.usage) } label: {
                    Text(usage).font(MadType.label).foregroundStyle(.secondary).lineLimit(1)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("用量，\(usage)")
            }
            Spacer(minLength: 0)
            HomeMenu(groups: menu, action: actions.menu)
                .menuStyle(.borderlessButton)
                .menuIndicator(.hidden)
                .fixedSize()
                .padding(8)
                .background(Color.chatComposer, in: Circle())
                .overlay(Circle().strokeBorder(Color.chatLine.opacity(0.7)))
        }
        .padding(.horizontal, 14).padding(.vertical, 10)
    }
}
#endif
