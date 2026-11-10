import SwiftUI

#if os(macOS)
/// What waits on the person, as a compact card of the list's 待处理 section: the agent's avatar (amber ring), its name
/// and how long it has waited; the command (or the question) in a box of code; Allow and Deny, or Reply for a
/// question (it opens the agent), and the project at the right. A tap on the card opens the agent.
struct WaitingCard: View {
    let item: HomeWaiting
    let actions: HomeActions
    /// Whether the command box had to cut the command: then Allow is not offered beside it, Review is (it opens the
    /// agent, whose card shows the whole command). Until the box is measured it counts as cut.
    @State private var cut = true

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            HStack(spacing: 8) {
                CharacterView(id: item.agentID, kind: item.agentKind, state: .needsYou, size: 26)
                Text(item.name)
                    .font(.system(size: 15.5, weight: .semibold))
                    .foregroundStyle(Color.madFg)
                    .lineLimit(1)
                    .frame(maxWidth: .infinity, alignment: .leading)
                Text(item.age).font(.system(size: 13.5)).monospacedDigit().foregroundStyle(Color.madFg3)
            }
            if item.mustOpen, !item.detail.isEmpty {
                Text(item.macLine.isEmpty ? item.detail : item.macLine).font(.system(size: 14)).foregroundStyle(Color.madFg2).lineLimit(item.macLine.isEmpty ? 1 : 2)
            } else if !item.detail.isEmpty {
                MacCommandLine(text: item.detail, mono: item.mono, cut: $cut)
            }
            HStack(spacing: 6) {
                if item.mustOpen {
                    Button(L("查看")) { actions.open(item.viewID) }.buttonStyle(MacPillStyle(primary: true))
                    if let deny = item.deny { Button(deny.label) { actions.act(item.id, deny.id, nil) }.buttonStyle(MacPillStyle(primary: false)) }
                } else if item.isPermission {
                    if cut && !item.detail.isEmpty {
                        Button(L("查看")) { actions.open(item.agentID) }.buttonStyle(MacPillStyle(primary: true))
                            .help(L("允许之前先看完全部内容"))
                    } else if let allow = item.allow {
                        Button(allow.label) { actions.act(item.id, allow.id, nil) }.buttonStyle(MacPillStyle(primary: true))
                    }
                    if let deny = item.deny {
                        Button(deny.label) { actions.act(item.id, deny.id, nil) }.buttonStyle(MacPillStyle(primary: false))
                    }
                } else {
                    Button(L("回复")) { actions.open(item.agentID) }.buttonStyle(MacPillStyle(primary: false))
                }
                Spacer(minLength: 0)
                if item.macLine.isEmpty { Text(item.project).font(.system(size: 13.5)).foregroundStyle(Color.madFg3).lineLimit(1) }
            }
        }
        .padding(.horizontal, 13)
        .padding(.vertical, 12)
        .background(Color.madWaitBg, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .onTapGesture { actions.open(item.agentID) }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("\(item.name), needs you, \(item.detail)")
    }
}

/// A section's head in the list: "待处理 2" in amber with its dot, "群聊" with a +, a project with its git and cost.
struct ListHead<Trailing: View>: View {
    let title: String
    var count: Int?
    var attention = false
    @ViewBuilder var trailing: Trailing

    var body: some View {
        HStack(spacing: 6) {
            if attention { StateDot(color: .madWait) }
            Text(title).fontWeight(attention ? .semibold : .regular)
            if let count { Text(verbatim: "\(count)").fontWeight(.medium) }
            Spacer(minLength: 8)
            trailing.fontWeight(.medium)
        }
        .font(Mac.sectionHead)
        .monospacedDigit()
        .foregroundStyle(attention ? Color.madWait : Color.madFg3)
        .lineLimit(1)
        .padding(.horizontal, 10)
        .padding(.top, 18)
        .padding(.bottom, 6)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }
}

/// A project's git and cost, the right side of its head: `main · 3 改动 · ↑2 · $4.17` (the branch in mono).
struct ProjectMetaLine: View {
    let meta: HomeProjectMeta

    var body: some View {
        HStack(spacing: 8) {
            if !meta.branch.isEmpty { Text(meta.branch).lineLimit(1) }
            ForEach(meta.parts, id: \.self) { Text($0).fixedSize() }
        }
        .font(Mac.sectionMeta)
    }
}

/// The list's 今日 row: a calendar in a soft tile, "今日", and how many wait, work and just finished.
struct TodayRowView: View {
    let today: TodayModel?
    let waiting: Int
    var metrics = RowMetrics.mac

    var body: some View {
        HStack(spacing: metrics.spacing) {
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(Color.macFloatFill)
                .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(Color.madLine, lineWidth: 1))
                .frame(width: metrics.avatar, height: metrics.avatar)
                .overlay(Image(systemName: "calendar").font(.system(size: metrics.avatar * 0.46)).foregroundStyle(Color.madFg2))
            VStack(alignment: .leading, spacing: 2) {
                Text(L("今日")).font(metrics.name).foregroundStyle(Color.madFg).lineLimit(1)
                Text(Lf("%lld 等你 · %lld 在工作 · %lld 刚完成", waiting, today?.working ?? 0, today?.done ?? 0))
                    .font(metrics.line).foregroundStyle(Color.madFg3).lineLimit(1)
            }
            Spacer(minLength: 0)
        }
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }
}
#endif
