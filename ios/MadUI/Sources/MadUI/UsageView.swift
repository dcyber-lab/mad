import SwiftUI

/// What the Usage screen shows: the subscriptions' windows, what the agents have cost by project and the agents that
/// cost the most, and mad's own budget for the day.
public struct UsageScreen: Equatable, Sendable {
    public var quotas: [UsageQuota]
    /// What today's responses cost over every session on the Mac: "$6.82"; empty when not known.
    public var total: String
    public var projects: [UsageLine]
    public var agents: [UsageLine]
    public var budget: UsageBudget?

    public init(quotas: [UsageQuota] = [], total: String = "", projects: [UsageLine] = [], agents: [UsageLine] = [], budget: UsageBudget? = nil) {
        self.quotas = quotas; self.total = total; self.projects = projects; self.agents = agents; self.budget = budget
    }
}

/// One window of a subscription: "Claude · 5 小时", 42%, "2:10 后重置". Amber above 80%.
public struct UsageQuota: Identifiable, Equatable, Sendable {
    public var id: String { title + window }
    public var title: String
    public var window: String
    public var percent: Int
    /// "2:10 后重置"; empty when not known.
    public var reset: String

    public init(title: String, window: String, percent: Int, reset: String = "") {
        self.title = title; self.window = window; self.percent = percent; self.reset = reset
    }

    public var hot: Bool { percent > 80 }
}

/// A project or an agent and what it cost. An agent's line has its picture (`agentKind` and `state`).
public struct UsageLine: Identifiable, Equatable, Sendable {
    public var id: String
    public var name: String
    /// "`main` · 5 个 agent", "mad · claude".
    public var sub: String
    /// The part of `sub` set in mono (a branch), at its start; empty for none.
    public var mono: String
    public var number: String
    public var agent: Bool
    public var agentKind: String
    public var state: CharacterState

    public init(id: String, name: String, sub: String = "", mono: String = "", number: String = "", agent: Bool = false,
                agentKind: String = "", state: CharacterState = .idle) {
        self.id = id; self.name = name; self.sub = sub; self.mono = mono; self.number = number; self.agent = agent
        self.agentKind = agentKind; self.state = state
    }
}

/// mad's own agents' budget for the day: "每天 $20 · 已用 $6.82" and how much of it is spent.
public struct UsageBudget: Equatable, Sendable {
    public var note: String
    /// 0 to 1.
    public var part: Double
    public var closed: Bool
    /// Lifted for the day already: 今天不限 is not offered.
    public var open: Bool

    public init(note: String, part: Double, closed: Bool = false, open: Bool = false) {
        self.note = note; self.part = part; self.closed = closed; self.open = open
    }

    public var hot: Bool { part >= 0.8 }
}

/// What the Usage screen asks of the app.
public struct UsageActions {
    /// 今天不限: mad's own budget is lifted for the day.
    public var liftToday: () -> Void
    public var openAgent: (String) -> Void
    /// The phone's way back; nil draws none.
    public var back: (() -> Void)?

    public init(liftToday: @escaping () -> Void = {}, openAgent: @escaping (String) -> Void = { _ in }, back: (() -> Void)? = nil) {
        self.liftToday = liftToday; self.openAgent = openAgent; self.back = back
    }
}

/// The Usage screen, as the approved design (v5) draws it: four limit cards (Claude and Codex, five hours and the
/// week, with the percent, a bar and when it resets; amber above 80%), what was spent by project, the agents that cost
/// the most, mad's own budget as a bar, and 今天不限.
public struct UsageView: View {
    let screen: UsageScreen
    let actions: UsageActions

    public init(screen: UsageScreen, actions: UsageActions = UsageActions()) {
        self.screen = screen
        self.actions = actions
    }

    public var body: some View {
        #if os(iOS)
        phoneBody
        #else
        macBody
        #endif
    }

    #if os(macOS)
    private var macBody: some View {
        VStack(spacing: 0) {
            head
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    block(L("订阅额度"), note: L("超过 80% 变琥珀色")) {
                        if screen.quotas.isEmpty {
                            quiet(L("还没有额度数据"))
                        } else {
                            LazyVGrid(columns: [GridItem(.flexible(), spacing: 12), GridItem(.flexible(), spacing: 12)], spacing: 12) {
                                ForEach(screen.quotas) { QuotaCard(quota: $0) }
                            }
                        }
                    }
                    block(L("会话花费"), note: screen.total.isEmpty ? "" : Lf("今天所有会话共 %@", screen.total)) {
                        if screen.projects.isEmpty { quiet(L("还没有花费")) } else { lines(screen.projects) }
                    }
                    if !screen.agents.isEmpty {
                        block(L("花得最多的 agent"), note: "") { lines(screen.agents) }
                    }
                    if let b = screen.budget {
                        block(L("mad 自己的 agent 预算"), note: b.note) {
                            Bar(part: b.part, hot: b.hot || b.closed)
                            if b.closed { Text(L("已用完，agent 在等")).font(.mad(12)).foregroundStyle(Color.madWait) }
                        }
                    }
                }
                .frame(maxWidth: 760, alignment: .leading)
                .padding(.horizontal, padding)
                .padding(.top, 22)
                .padding(.bottom, 28)
                .frame(maxWidth: .infinity)
            }
        }
        .background(Color.madPaper)
        .environment(\.madAvatarBackdrop, Color.madPaper)
    }
    #endif

    private var padding: CGFloat { 28 }

    private var head: some View {
        PageHead(title: L("用量"), sub: L("订阅额度和今天的花费（按 API 价格估算）")) { lift }
    }

    @ViewBuilder private var lift: some View {
        if let b = screen.budget, !b.open {
            Button(L("今天不限"), action: actions.liftToday).buttonStyle(.mad())
        }
    }

    private func block<Content: View>(_ title: String, note: String, @ViewBuilder _ content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Text(title).font(.mad(12.5, weight: .semibold)).foregroundStyle(Color.madFg2)
                Spacer(minLength: 0)
                if !note.isEmpty { Text(note).font(.mad(11.5)).foregroundStyle(Color.madFg3).lineLimit(1) }
            }
            content()
        }
    }

    private func quiet(_ text: String) -> some View {
        Text(text).font(.mad(12.5)).foregroundStyle(Color.madFg3).padding(.horizontal, 2)
    }

    private func lines(_ items: [UsageLine]) -> some View {
        VStack(spacing: 0) {
            ForEach(Array(items.enumerated()), id: \.element.id) { i, line in
                if i > 0 { Rectangle().fill(Color.madLine).frame(height: 1) }
                HStack(spacing: 12) {
                    if line.agent {
                        CharacterView(id: line.id, kind: line.agentKind, state: line.state, size: 28)
                    } else {
                        RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Color.madRaise).frame(width: 28, height: 28)
                    }
                    VStack(alignment: .leading, spacing: 0) {
                        Text(line.name).font(.mad(13, weight: .medium)).foregroundStyle(Color.madFg).lineLimit(1)
                        sub(line).font(.mad(12)).lineLimit(1)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    Text(line.number).font(.mad(12)).monospacedDigit().foregroundStyle(Color.madFg3)
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 9)
                .contentShape(Rectangle())
                .onTapGesture { if line.agent { actions.openAgent(line.id) } }
            }
        }
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Color.madLine, lineWidth: 1))
        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
    }

    private func sub(_ line: UsageLine) -> Text {
        let rest = Text(line.sub).foregroundColor(.madFg3)
        guard !line.mono.isEmpty else { return rest }
        let mono = Text(line.mono).font(.mad(11.5, design: .monospaced)).foregroundColor(.madFg2)
        return line.sub.isEmpty ? mono : mono + Text(" · ").foregroundColor(.madFg3) + rest
    }
}

/// One window's card: "Claude · 5 小时" and when it resets, the percent large, a bar (amber above 80%).
struct QuotaCard: View {
    let quota: UsageQuota

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Text(verbatim: "\(quota.title) · \(quota.window)").foregroundStyle(Color.madFg2).lineLimit(1)
                Spacer(minLength: 0)
                Text(quota.reset).monospacedDigit().foregroundStyle(Color.madFg3).lineLimit(1)
            }
            .font(.mad(12.5))
            Text(verbatim: "\(quota.percent)%").font(.system(size: 24, weight: .semibold)).monospacedDigit()
                .foregroundStyle(Color.madFg)
            Bar(part: Double(quota.percent) / 100, hot: quota.hot)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Color.madLine, lineWidth: 1))
        .accessibilityElement(children: .combine)
    }
}

/// A 6pt bar: the part filled in the stronger grey, amber when hot.
struct Bar: View {
    let part: Double
    let hot: Bool

    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Capsule().fill(Color.madRaise2)
                Capsule().fill(hot ? Color.madWait : Color.madFg2).frame(width: g.size.width * min(1, max(0, part)))
            }
        }
        .frame(height: 6)
    }
}
