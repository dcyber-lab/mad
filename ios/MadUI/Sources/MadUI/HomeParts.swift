import SwiftUI

/// What waits on the person, as a compact card of the list's 待处理 section: the agent's avatar (amber ring), its name
/// and how long it has waited; the command (or the question) in a box of code; Allow and Deny, or Reply for a
/// question (it opens the agent), and the project at the right. A tap on the card opens the agent.
struct WaitingCard: View {
    let item: HomeWaiting
    let actions: HomeActions
    /// The phone's card is larger (28pt avatar, 15pt name, 14pt buttons), and a question shows only its words.
    var phone = false

    var body: some View {
        VStack(alignment: .leading, spacing: phone ? 8 : 6) {
            HStack(spacing: 8) {
                CharacterView(id: item.agentID, kind: item.agentKind, state: .needsYou, size: phone ? 28 : 24)
                Text(item.name)
                    .font(phone ? .system(size: 15, weight: .semibold) : .system(size: 13, weight: .medium))
                    .foregroundStyle(Color.madFg)
                    .lineLimit(1)
                    .frame(maxWidth: .infinity, alignment: .leading)
                Text(item.age).font(.system(size: phone ? 12 : 11)).monospacedDigit().foregroundStyle(Color.madFg3)
            }
            if phone && !item.isPermission {
                Text(item.detail).font(.system(size: 14)).foregroundStyle(Color.madFg).lineLimit(2)
            } else if !item.detail.isEmpty {
                CommandBox(text: item.detail, font: item.mono
                           ? .system(size: phone ? 12.5 : 11.5, design: .monospaced)
                           : .system(size: phone ? 12.5 : 11.5))
            }
            if !(phone && !item.isPermission) {
                HStack(spacing: 6) {
                    if item.isPermission {
                        if let allow = item.allow {
                            Button(allow.label) { actions.act(item.id, allow.id, nil) }.buttonStyle(.mad(.primary, size))
                        }
                        if let deny = item.deny {
                            Button(deny.label) { actions.act(item.id, deny.id, nil) }.buttonStyle(.mad(.normal, size))
                        }
                    } else {
                        Button(L("回复")) { actions.open(item.agentID) }.buttonStyle(.mad(.normal, size))
                    }
                    Spacer(minLength: 0)
                    Text(item.project).font(.system(size: phone ? 12 : 11)).foregroundStyle(Color.madFg3).lineLimit(1)
                }
            }
        }
        .padding(.horizontal, phone ? 12 : 10)
        .padding(.vertical, phone ? 10 : 8)
        .background(Color.madPaper, in: RoundedRectangle(cornerRadius: phone ? 14 : 10, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: phone ? 14 : 10, style: .continuous).strokeBorder(Color.madLine, lineWidth: 1))
        .contentShape(RoundedRectangle(cornerRadius: phone ? 14 : 10, style: .continuous))
        .onTapGesture { actions.open(item.agentID) }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("\(item.name), needs you, \(item.detail)")
    }

    private var size: MadButtonStyle.Size {
        #if os(iOS)
        phone ? .regular : .small
        #else
        .small
        #endif
    }
}

/// A section's head in the list: "待处理 2" in amber with its dot, "群聊" with a +, a project with its git and cost.
struct ListHead<Trailing: View>: View {
    let title: String
    var count: Int?
    var attention = false
    var phone = false
    @ViewBuilder var trailing: Trailing

    var body: some View {
        HStack(spacing: 6) {
            if attention { StateDot(color: .madWait) }
            Text(title).fontWeight(.semibold)
            if let count { Text(verbatim: "\(count)").fontWeight(.medium) }
            Spacer(minLength: 8)
            trailing.fontWeight(.medium)
        }
        .font(.system(size: phone ? 12.5 : 11.5))
        .monospacedDigit()
        .foregroundStyle(attention ? Color.madWait : Color.madFg3)
        .lineLimit(1)
        .padding(.horizontal, 8)
        .padding(.top, 14)
        .padding(.bottom, 4)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }
}

/// A project's git and cost, the right side of its head: `main · 3 改动 · ↑2 · $4.17` (the branch in mono).
struct ProjectMetaLine: View {
    let meta: HomeProjectMeta

    var body: some View {
        HStack(spacing: 8) {
            if !meta.branch.isEmpty { Text(meta.branch).font(.system(size: 11.5, design: .monospaced)).lineLimit(1) }
            ForEach(meta.parts, id: \.self) { Text($0).fixedSize() }
        }
    }
}

/// The list's 今日 row: a calendar in a soft tile, "今日", and how many wait, work and just finished.
struct TodayRowView: View {
    let today: TodayModel?
    let waiting: Int
    var metrics = RowMetrics.mac

    var body: some View {
        HStack(spacing: metrics.spacing) {
            RoundedRectangle(cornerRadius: metrics.avatar * 0.31, style: .continuous)
                .fill(Color.madRaise)
                .frame(width: metrics.avatar, height: metrics.avatar)
                .overlay(Image(systemName: "calendar").font(.system(size: metrics.avatar * 0.5)).foregroundStyle(Color.madFg2))
            VStack(alignment: .leading, spacing: 1) {
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

/// How the Mac's sidebar marks the open row: paper with a hairline and a soft shadow, rounded 10.
struct SelectedRowBackground: ViewModifier {
    let selected: Bool

    func body(content: Content) -> some View {
        content
            .background {
                if selected {
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .fill(Color.madPaper)
                        .shadow(color: .black.opacity(0.06), radius: 1, y: 1)
                        .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Color.madLine, lineWidth: 1))
                }
            }
    }
}
