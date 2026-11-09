import SwiftUI

#if os(iOS)
/// The phone home's 待处理 card (design v6): amber tint, 22pt corners, the agent's avatar with its amber ring, name and
/// wait time, what it wants, the command in a mono box that is cut inside the card (never widens it), and 允许 / 拒绝 /
/// 查看 ›. A command the box had to cut is not allowed from here: 查看 opens the agent, where the whole command is.
struct PhoneWaitingCard: View {
    let items: [HomeWaiting]
    let actions: HomeActions

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 8) {
                StateDot(color: .madWait).scaleEffect(1.3)
                Text(L("待处理")).font(.system(size: 15, weight: .semibold)).foregroundStyle(Color.madWait)
                Spacer(minLength: 0)
                Text(verbatim: "\(items.count)").font(.system(size: 14, weight: .medium)).monospacedDigit().foregroundStyle(Color.madFg3)
            }
            ForEach(items) { item in
                PhoneWaitingTask(item: item, actions: actions)
            }
        }
        .padding(.horizontal, 16)
        .padding(.top, 16)
        .padding(.bottom, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.madWaitBg, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
        .accessibilityElement(children: .contain)
    }
}

private struct PhoneWaitingTask: View {
    let item: HomeWaiting
    let actions: HomeActions
    @State private var cut = true

    /// "想运行一条命令": the last part of the item's where-line.
    private var what: String { item.whereLine.components(separatedBy: " · ").last ?? "" }

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            CharacterView(id: item.agentID, kind: item.agentKind, state: .needsYou, size: 40)
            VStack(alignment: .leading, spacing: 0) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(item.name).font(.system(size: 17, weight: .semibold)).foregroundStyle(Color.madFg).lineLimit(1)
                    Spacer(minLength: 0)
                    Text(item.age).font(.system(size: 14)).monospacedDigit().foregroundStyle(Color.madFg3).lineLimit(1).fixedSize()
                }
                if !what.isEmpty {
                    Text(what).font(.system(size: 15)).foregroundStyle(Color.madFg2).lineLimit(1).padding(.top, 2)
                }
                if item.isPermission {
                    if !item.detail.isEmpty { PhoneCommandBox(text: item.detail, mono: item.mono, cut: $cut).padding(.top, 8) }
                } else if !item.detail.isEmpty {
                    Text(item.detail).font(.system(size: 15)).foregroundStyle(Color.madFg).lineLimit(2).padding(.top, 6)
                }
                buttons.padding(.top, 10)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .contentShape(Rectangle())
        .onTapGesture { actions.open(item.agentID) }
    }

    private var buttons: some View {
        HStack(spacing: 8) {
            if item.mustOpen {
                pill(L("查看"), primary: true) { actions.open(item.agentID) }
            } else if item.isPermission {
                if cut && !item.detail.isEmpty {
                    // the whole command is read before it is allowed
                    pill(L("查看"), primary: true) { actions.open(item.agentID) }
                } else if let allow = item.allow {
                    pill(allow.label, primary: true) { actions.act(item.id, allow.id, nil) }
                }
                if let deny = item.deny { pill(deny.label, primary: false) { actions.act(item.id, deny.id, nil) } }
                if !(cut && !item.detail.isEmpty) {
                    Button { actions.open(item.agentID) } label: {
                        Text(L("查看 ›")).font(.system(size: 15, weight: .medium)).foregroundStyle(Color.madFg3).padding(.horizontal, 6)
                            .frame(height: 38)
                    }
                    .buttonStyle(.plain)
                }
            } else {
                pill(L("回复"), primary: false) { actions.open(item.agentID) }
            }
            Spacer(minLength: 0)
        }
    }

    private func pill(_ title: String, primary: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title).font(.system(size: 15, weight: .semibold)).lineLimit(1)
                .foregroundStyle(primary ? Color.madPaper : Color.madFg)
                .padding(.horizontal, 18)
                .frame(height: 38)
                .background(primary ? Color.madFg : Color.madPaper, in: Capsule())
                .overlay(Capsule().strokeBorder(primary ? Color.clear : Color.madLine2, lineWidth: 1))
                .contentShape(Capsule())
        }
        .buttonStyle(.plain)
    }
}

/// The command in a 14pt mono box, one line cut at its end inside the card; `cut` says whether it had to be cut.
private struct PhoneCommandBox: View {
    let text: String
    let mono: Bool
    @Binding var cut: Bool
    @State private var whole: CGFloat = .infinity
    @State private var room: CGFloat = 0

    private var font: Font { .system(size: 14, design: mono ? .monospaced : .default) }

    var body: some View {
        Text(text)
            .font(font)
            .foregroundStyle(Color.madFg2)
            .lineLimit(1)
            .truncationMode(.tail)
            .padding(.horizontal, 11)
            .padding(.vertical, 9)
            .frame(maxWidth: .infinity, alignment: .leading)
            .onGeometryChange(for: CGFloat.self) { $0.size.width - 22 } action: { room = $0; report() }
            .background(alignment: .leading) {
                Text(text).font(font).lineLimit(1).fixedSize().hidden()
                    .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { whole = $0; report() }
            }
            .background(Color.madPaper.opacity(0.75), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
            .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
    }

    private func report() {
        let isCut = whole > room + 0.5
        if cut != isCut { cut = isCut }
    }
}

/// A section's small grey head on the phone: the name at the left, a project's `main · $689` in mono at the right.
struct PhoneSectionHead: View {
    let title: String
    var meta: HomeProjectMeta?

    var body: some View {
        HStack(spacing: 8) {
            Text(title).font(Phone.sectionHead).foregroundStyle(Color.madFg3).lineLimit(1)
            Spacer(minLength: 8)
            if let meta, let line = Self.line(meta) {
                Text(line).font(.system(size: 13, design: .monospaced)).foregroundStyle(Color.madFg3).lineLimit(1)
            }
        }
        .padding(.horizontal, 24)
        .padding(.top, 20)
        .padding(.bottom, 2)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }

    /// "main · $689": the branch and what the project's agents cost.
    static func line(_ meta: HomeProjectMeta) -> String? {
        let parts = [meta.branch, meta.cost > 0 ? MadWords.money(meta.cost) : ""].filter { !$0.isEmpty }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }
}

/// The bottom sheet the round grid button opens (design v6): the person and the Mac, four big tiles, a list.
struct PhoneMenuSheet: View {
    let person: String
    let machine: String
    let groups: [[HomeMenuItem]]
    /// Called with the chosen item; the sheet closes first.
    let choose: (HomeMenuItem) -> Void

    private static let tileItems: [HomeMenuItem] = [.usage, .board, .skills, .notifications]

    private var all: [HomeMenuItem] { groups.flatMap { $0 } }
    private var tiles: [HomeMenuItem] { Self.tileItems.filter(all.contains) }
    private var list: [HomeMenuItem] { all.filter { !Self.tileItems.contains($0) } }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 12) {
                PersonPicture(name: person, size: 48)
                VStack(alignment: .leading, spacing: 1) {
                    Text(person).font(.system(size: 19, weight: .semibold)).foregroundStyle(Color.madFg).lineLimit(1)
                    Text(machine).font(.system(size: 14)).foregroundStyle(Color.madFg3).lineLimit(1).truncationMode(.tail)
                }
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 4)
            .padding(.bottom, 16)
            Divider()
            HStack(alignment: .top, spacing: 6) {
                ForEach(tiles, id: \.self) { item in
                    Button { choose(item) } label: {
                        VStack(spacing: 8) {
                            Image(systemName: Self.symbol(item)).font(.system(size: 24, weight: .medium)).foregroundStyle(Color.madFg)
                                .frame(width: 56, height: 56)
                                .background(Color.madRaise, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
                            Text(item.title).font(.system(size: 13)).foregroundStyle(Color.madFg2).lineLimit(1)
                        }
                        .frame(maxWidth: .infinity)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.top, 20)
            .padding(.bottom, 6)
            VStack(spacing: 0) {
                ForEach(list, id: \.self) { item in
                    Divider()
                    Button { choose(item) } label: {
                        HStack {
                            Text(item.title).font(.system(size: 17)).foregroundStyle(Color.madFg)
                            Spacer(minLength: 0)
                            Image(systemName: "chevron.right").font(.system(size: 13, weight: .semibold)).foregroundStyle(Color.madFg3)
                        }
                        .padding(.vertical, 15).padding(.horizontal, 4)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.top, 14)
        }
        .padding(.horizontal, 18)
        .padding(.top, 22)
        .padding(.bottom, 8)
    }

    private static func symbol(_ item: HomeMenuItem) -> String {
        switch item {
        case .usage: "chart.bar"
        case .board: "rectangle.split.3x1"
        case .skills: "star"
        case .notifications: "bell"
        default: "circle"
        }
    }
}
#endif

