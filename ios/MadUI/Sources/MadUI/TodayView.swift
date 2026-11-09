import SwiftUI

/// The Mac's first screen (the approved design's 今日, which replaces the overview): one sentence of how things stand
/// in the state colors and what was spent; what waits as amber-tinted cards with the command and its acts; what
/// works and what just finished as compact bordered lists; an empty section is one grey line; the keys at the foot.
public struct TodayView: View {
    let model: HomeModel
    let actions: HomeActions

    public init(model: HomeModel, actions: HomeActions = HomeActions()) {
        self.model = model
        self.actions = actions
    }

    private var today: TodayModel { model.today ?? TodayModel(date: MadWords.today(Date())) }

    public var body: some View {
        VStack(spacing: 0) {
            PageHead(title: L("今日"), sub: today.date) {
                Button { actions.create(.chat) } label: { HStack(spacing: 4) { Text(L("新对话")); Kbd("⌘N") } }
                    .buttonStyle(.mad())
                    .keyboardShortcut("n", modifiers: .command)
                Button { actions.menu(.board) } label: { HStack(spacing: 4) { Text(L("看板")); Kbd("⌥B") } }
                    .buttonStyle(.mad())
                    .keyboardShortcut("b", modifiers: .option)
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    summary
                    block(L("在等你"), color: .madWait, keys: true) {
                        ForEach(model.waiting) { WaitingTask(item: $0, actions: actions) }
                        if model.waiting.isEmpty { quiet(L("没有在等你的")) }
                    }
                    block(L("在工作"), color: .madWork) {
                        if today.workingLines.isEmpty { quiet(L("没有在工作的")) } else {
                            LinesBox(lines: today.workingLines, actions: actions, button: L("打断")) { actions.interrupt($0) }
                        }
                    }
                    block(L("刚完成"), color: .madDone) {
                        if today.doneLines.isEmpty { quiet(L("没有刚完成的")) } else {
                            LinesBox(lines: today.doneLines, actions: actions, button: L("看改动")) { actions.open($0) }
                        }
                    }
                    keys
                }
                .frame(maxWidth: 760, alignment: .leading)
                .padding(.horizontal, 28)
                .padding(.top, 22)
                .padding(.bottom, 28)
                .frame(maxWidth: .infinity)
            }
        }
        .background(Color.madPaper)
        .environment(\.madAvatarBackdrop, Color.madPaper)
    }

    /// "2 个在等你，2 个在工作，1 个刚完成。今天花了 $6.82。", each count bold in its state's color.
    private var summary: some View {
        var t = Text(Lf("%lld 个在等你", model.waiting.count)).bold().foregroundColor(.madWait)
            + Text(L("，"))
            + Text(Lf("%lld 个在工作", today.working)).bold().foregroundColor(.madWork)
            + Text(L("，"))
            + Text(Lf("%lld 个刚完成", today.done)).bold().foregroundColor(.madDone)
            + Text(L("。"))
        if !today.spend.isEmpty { t = t + Text(Lf("今天花了 %@。", today.spend)) }
        return t.font(.system(size: 13.5)).foregroundColor(.madFg2).fixedSize(horizontal: false, vertical: true)
    }

    private func block<Content: View>(_ title: String, color: Color, keys: Bool = false, @ViewBuilder _ content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                StateDot(color: color)
                Text(title).font(.system(size: 12.5, weight: .semibold)).foregroundStyle(Color.madFg2)
                Spacer(minLength: 0)
                if keys {
                    HStack(spacing: 4) {
                        Text(L("按等待时间 ·"))
                        Kbd("⌥N")
                        Text(L("下一个"))
                    }
                    .font(.system(size: 11.5))
                    .foregroundStyle(Color.madFg3)
                }
            }
            content()
        }
    }

    private func quiet(_ text: String) -> some View {
        Text(text).font(.system(size: 12.5)).foregroundStyle(Color.madFg3).padding(.horizontal, 2)
    }

    private var keys: some View {
        HStack(spacing: 14) {
            key("⌥N", L("下一个等你的"))
            key("⌘K", L("跳转"))
            key("⌘\\", L("侧栏"))
            key("⌘I", L("详情"))
        }
        .font(.system(size: 11.5))
        .foregroundStyle(Color.madFg3)
        .frame(maxWidth: .infinity)
        .padding(.top, 6)
    }

    private func key(_ k: String, _ what: String) -> some View {
        HStack(spacing: 3) { Kbd(k); Text(what) }
    }
}

/// A screen's head bar (the design's `.mh`): a title and its sub line at the left, buttons at the right, a hairline
/// under it.
struct PageHead<Trailing: View>: View {
    let title: String
    let sub: String
    @ViewBuilder var trailing: Trailing
    @Environment(\.madHeadLeading) private var leading

    var body: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 0) {
                Text(title).font(.system(size: 15, weight: .semibold)).foregroundStyle(Color.madFg).lineLimit(1)
                if !sub.isEmpty { Text(sub).font(.system(size: 12)).foregroundStyle(Color.madFg3).lineLimit(1) }
            }
            Spacer(minLength: 8)
            HStack(spacing: 8) { trailing }
        }
        .padding(.leading, 20 + leading)
        .padding(.trailing, 20)
        .padding(.vertical, 12)
        .frame(minHeight: 60)
        .overlay(alignment: .bottom) { Rectangle().fill(Color.madLine).frame(height: 1) }
        .accessibilityElement(children: .contain)
    }
}

private struct HeadLeadingKey: EnvironmentKey { static let defaultValue: CGFloat = 0 }

extension EnvironmentValues {
    /// Room a head bar keeps at its left for what floats over it (the Mac's show-sidebar button while the sidebar is
    /// hidden).
    public var madHeadLeading: CGFloat {
        get { self[HeadLeadingKey.self] }
        set { self[HeadLeadingKey.self] = newValue }
    }
}

/// What waits, as a Today card: the avatar (amber ring), the name, where it is and what it wants; the command in a box
/// of code; Allow, Allow for this session, Deny and Open chat (a question: its options and Reply…); at the right how
/// long it has waited and what it cost. Tinted amber.
struct WaitingTask: View {
    let item: HomeWaiting
    let actions: HomeActions

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            CharacterView(id: item.agentID, kind: item.agentKind, state: .needsYou, size: 36)
            VStack(alignment: .leading, spacing: 3) {
                Text(item.name).font(.system(size: 13.5, weight: .semibold)).foregroundStyle(Color.madFg).lineLimit(1)
                if !item.whereLine.isEmpty { Text(item.whereLine).font(.system(size: 12)).foregroundStyle(Color.madFg3).lineLimit(1) }
                if item.isPermission || item.mono {
                    ScrollView(.horizontal, showsIndicators: false) {
                        Text(item.detail).font(.system(size: 12, design: .monospaced)).foregroundStyle(Color.madFg).fixedSize()
                            .padding(.horizontal, 9).padding(.vertical, 6)
                    }
                    .background(Color.madCode, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(Color.madLine, lineWidth: 1))
                    .padding(.top, 4)
                } else if !item.detail.isEmpty {
                    Text(item.detail).font(.system(size: 13)).foregroundStyle(Color.madFg).padding(.top, 4)
                        .fixedSize(horizontal: false, vertical: true)
                }
                FlowLayout(spacing: 6) {
                    if item.isPermission {
                        if let a = item.allow { Button(a.label) { actions.act(item.id, a.id, nil) }.buttonStyle(.mad(.primary)) }
                        if let a = item.allowSession { Button(a.label) { actions.act(item.id, a.id, nil) }.buttonStyle(.mad()) }
                        if let a = item.deny { Button(a.label) { actions.act(item.id, a.id, nil) }.buttonStyle(.mad()) }
                        Button(L("打开对话")) { actions.open(item.agentID) }.buttonStyle(.mad(.ghost))
                    } else {
                        ForEach(item.options) { a in Button(a.label) { actions.act(item.id, a.id, nil) }.buttonStyle(.mad()) }
                        Button(L("回复…")) { actions.open(item.agentID) }.buttonStyle(.mad(.ghost))
                    }
                }
                .padding(.top, 6)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            VStack(alignment: .trailing, spacing: 4) {
                Text(item.age)
                if !item.cost.isEmpty { Text(item.cost) }
            }
            .font(.system(size: 11.5))
            .monospacedDigit()
            .foregroundStyle(Color.madFg3)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .background {
            // the waiting tint at 45% over paper
            ZStack {
                RoundedRectangle(cornerRadius: 12, style: .continuous).fill(Color.madPaper)
                RoundedRectangle(cornerRadius: 12, style: .continuous).fill(Color.madWaitBg.opacity(0.45))
            }
        }
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Color.madWait.opacity(0.35), lineWidth: 1))
        .accessibilityElement(children: .contain)
    }
}

/// Agents in one bordered box, a hairline between them: avatar, name, what it does, a number, one small button.
struct LinesBox: View {
    let lines: [TodayLine]
    let actions: HomeActions
    let button: String
    let act: (String) -> Void

    var body: some View {
        VStack(spacing: 0) {
            ForEach(Array(lines.enumerated()), id: \.element.id) { i, line in
                if i > 0 { Rectangle().fill(Color.madLine).frame(height: 1) }
                HStack(spacing: 12) {
                    CharacterView(id: line.id, kind: line.kind, state: line.state, size: 28)
                    VStack(alignment: .leading, spacing: 0) {
                        Text(line.name).font(.system(size: 13, weight: .medium)).foregroundStyle(Color.madFg).lineLimit(1)
                        sub(line).font(.system(size: 12)).lineLimit(1).truncationMode(.tail)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    Text(line.number).font(.system(size: 12)).monospacedDigit().foregroundStyle(Color.madFg3).lineLimit(1).fixedSize()
                    Button(button) { act(line.id) }.buttonStyle(.mad(.normal, .small))
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 9)
                .contentShape(Rectangle())
                .onTapGesture { actions.open(line.id) }
            }
        }
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Color.madLine, lineWidth: 1))
        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
    }

    /// "#ios · Edit · `MacSidebar.swift`", the target in mono; or the last line.
    private func sub(_ line: TodayLine) -> Text {
        var parts: [Text] = []
        if !line.place.isEmpty { parts.append(Text(line.place)) }
        if !line.tool.isEmpty { parts.append(Text(line.tool)) }
        if !line.detail.isEmpty {
            parts.append(line.tool.isEmpty ? Text(line.detail)
                         : Text(line.detail).font(.system(size: 11.5, design: .monospaced)).foregroundColor(.madFg2))
        }
        guard var t = parts.first else { return Text("") }
        for p in parts.dropFirst() { t = t + Text(" · ") + p }
        return t.foregroundColor(.madFg3)
    }
}
