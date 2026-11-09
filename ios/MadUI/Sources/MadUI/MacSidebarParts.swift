#if os(macOS)
import AppKit
import SwiftUI

/// The pieces of the Mac sidebar (design v6, docs/design/mac-v6.html) that are not a list row: the selection's grey,
/// the capsule buttons of the 待处理 card, its command line, the round + menu, and the footer's grid menu.

/// How the sidebar marks the open row: a soft grey, rounded 12 (no hairline, as Grok's).
struct SelectedRowBackground: ViewModifier {
    let selected: Bool

    func body(content: Content) -> some View {
        content.background {
            if selected { RoundedRectangle(cornerRadius: Mac.rowRadius, style: .continuous).fill(Color.macSelection) }
        }
    }
}

extension Color {
    /// The open row's grey on the sidebar.
    static var macSelection: Color { dynamic(light: 0xe9e9ec, dark: 0x26262b) }
}

/// A capsule button on the 待处理 card: 30pt, white with a hairline, or ink for the main act.
struct MacPillStyle: ButtonStyle {
    let primary: Bool

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 14, weight: .semibold))
            .lineLimit(1)
            .foregroundStyle(primary ? Color.madPaper : Color.madFg)
            .padding(.horizontal, 14)
            .frame(height: 30)
            .background(primary ? Color.madFg : Color.macFloatFill, in: Capsule())
            .overlay(Capsule().strokeBorder(primary ? Color.clear : Color.madLine2, lineWidth: 1))
            .contentShape(Capsule())
            .opacity(configuration.isPressed ? 0.7 : 1)
    }
}

/// The command (or question) of a card: one line of code on a white box, cut at the end. `cut` is set to whether
/// the line had to be cut, once it is laid out.
struct MacCommandLine: View {
    let text: String
    let mono: Bool
    @Binding var cut: Bool
    @State private var whole: CGFloat = .infinity
    @State private var room: CGFloat = 0

    private var font: Font { mono ? .system(size: 13.5, design: .monospaced) : .system(size: 13.5) }

    var body: some View {
        Text(text)
            .font(font)
            .foregroundStyle(Color.madFg2)
            .lineLimit(1)
            .truncationMode(.tail)
            .frame(maxWidth: .infinity, alignment: .leading)
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { room = $0; report() }
            .background(alignment: .leading) {
                Text(text).font(font).lineLimit(1).fixedSize().hidden()
                    .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { whole = $0; report() }
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 5)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.macCommandFill, in: RoundedRectangle(cornerRadius: 7, style: .continuous))
    }

    private func report() {
        let isCut = whole > room + 0.5
        if cut != isCut { cut = isCut }
    }
}

extension Color {
    static var macCommandFill: Color {
        Color(NSColor(name: nil) {
            $0.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua ? NSColor.black.withAlphaComponent(0.28) : NSColor.white.withAlphaComponent(0.8)
        })
    }
}

/// The + at the top: a round floating button that opens the new-item menu.
struct MacNewMenu: View {
    let action: (HomeNewItem) -> Void

    var body: some View {
        Menu {
            ForEach(HomeNewItem.allCases, id: \.self) { item in
                Button { action(item) } label: { Label(item.title, systemImage: item.symbol) }
            }
        } label: {
            Image(systemName: "plus")
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(Color.madFg)
                .frame(width: Mac.button, height: Mac.button)
                .background(Color.macFloatFill, in: Circle())
                .contentShape(Circle())
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .menuIndicator(.hidden)
        .fixedSize()
        .macFloat()
        .accessibilityLabel("新建")
    }
}

/// The small + at the end of the 群聊 head.
struct MacSmallNewMenu: View {
    let action: (HomeNewItem) -> Void

    var body: some View {
        Menu {
            ForEach(HomeNewItem.allCases, id: \.self) { item in
                Button { action(item) } label: { Label(item.title, systemImage: item.symbol) }
            }
        } label: {
            Image(systemName: "plus").font(.system(size: 13, weight: .medium)).foregroundStyle(Color.madFg3)
                .frame(width: 22, height: 22)
                .contentShape(Rectangle())
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .menuIndicator(.hidden)
        .fixedSize()
        .accessibilityLabel("新建")
    }
}

extension HomeMenuItem {
    var macSymbol: String {
        switch self {
        case .usage: "chart.bar"
        case .board: "rectangle.split.3x1"
        case .skills: "sparkles"
        case .devices: "iphone"
        case .notifications: "bell"
        case .switchMac: "laptopcomputer"
        case .addProject: "folder.badge.plus"
        case .theme: "circle.lefthalf.filled"
        }
    }
}

/// The footer's round grid button: it opens a popover with the sidebar's menu (what the right click offers too).
struct MacFooterMenu: View {
    let groups: [[HomeMenuItem]]
    let action: (HomeMenuItem) -> Void
    @State private var shown = false

    init(groups: [[HomeMenuItem]], action: @escaping (HomeMenuItem) -> Void) {
        self.groups = groups
        self.action = action
        #if DEBUG
        // `-sidebarMenu YES` starts with the menu open, to look at it
        _shown = State(initialValue: UserDefaults.standard.bool(forKey: "sidebarMenu"))
        #endif
    }

    var body: some View {
        MacRoundButton { shown.toggle() } label: { Image(systemName: "square.grid.2x2") }
            .accessibilityLabel("菜单")
            .popover(isPresented: $shown, arrowEdge: .top) {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(groups.enumerated()), id: \.offset) { i, group in
                        if i > 0 { Divider().padding(.vertical, 5).padding(.horizontal, 6) }
                        ForEach(group, id: \.self) { item in
                            MacMenuRow(item: item) { shown = false; action(item) }
                        }
                    }
                }
                .padding(6)
                .frame(width: 270)
            }
    }
}

private struct MacMenuRow: View {
    let item: HomeMenuItem
    let action: () -> Void
    @State private var hover = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Image(systemName: item.macSymbol).font(.system(size: 14)).foregroundStyle(Color.madFg2).frame(width: 20)
                Text(item.title).font(.system(size: 15)).foregroundStyle(Color.madFg)
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 8)
            .background(hover ? Color.macSelection : Color.clear, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hover = $0 }
    }
}
#endif
