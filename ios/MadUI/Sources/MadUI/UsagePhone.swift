import SwiftUI

#if os(iOS)
extension UsageView {
    /// The phone's Usage (design v6): a back button and a 用量 capsule over the content; today's spend large; then cards
    /// for the subscriptions' limits (amber above 80%), the agents that cost the most, and mad's own budget.
    var phoneBody: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                spend
                limits
                if !screen.agents.isEmpty { topAgents }
                if let b = screen.budget { budget(b) }
            }
            .padding(.horizontal, 16)
            .padding(.top, 4)
            .padding(.bottom, 40)
        }
        .background(Color.madPaper)
        .environment(\.madAvatarBackdrop, Color.madSide)
        .safeAreaInset(edge: .top, spacing: 0) {
            PhoneTopBar {
                if let back = actions.back { PhoneBackButton(action: back) } else { Color.clear.frame(width: Phone.button, height: Phone.button) }
            } center: {
                PhoneTitlePill(title: L("用量"))
            } trailing: {
                Color.clear.frame(width: Phone.button, height: Phone.button)
            }
        }
    }

    /// 今天花了 / $15.86 按 API 价格估算.
    private var spend: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(L("今天花了")).font(.system(size: 15)).foregroundStyle(Color.madFg3)
            if screen.total.isEmpty {
                Text(L("还没有花费")).font(.system(size: 17)).foregroundStyle(Color.madFg3)
            } else {
                ViewThatFits(in: .horizontal) {
                    HStack(alignment: .firstTextBaseline, spacing: 6) { total; note }
                    VStack(alignment: .leading, spacing: 0) { total; note }
                }
            }
        }
        .padding(.horizontal, 8)
        .padding(.top, 14)
        .padding(.bottom, 6)
        .accessibilityElement(children: .combine)
    }

    private var total: some View {
        Text(screen.total).font(.system(size: 44, weight: .bold)).tracking(-0.9).monospacedDigit().foregroundStyle(Color.madFg)
            .lineLimit(1).minimumScaleFactor(0.6)
    }

    private var note: some View {
        Text(L("按 API 价格估算")).font(.system(size: 17, weight: .medium)).foregroundStyle(Color.madFg3).lineLimit(1)
    }

    private var limits: some View {
        PhoneCard(L("订阅额度"), note: L("超过 80% 变琥珀色")) {
            if screen.quotas.isEmpty {
                Text(L("还没有额度数据")).font(.system(size: 15)).foregroundStyle(Color.madFg3).padding(.horizontal, 18).padding(.vertical, 12)
            } else {
                ForEach(screen.quotas) { q in
                    meter(label: "\(q.title) · \(q.window)", value: q.reset.isEmpty ? "\(q.percent)%" : "\(q.percent)% · \(q.reset)",
                          part: Double(q.percent) / 100, hot: q.hot)
                }
            }
        }
    }

    private var topAgents: some View {
        PhoneCard(L("今天花得最多")) {
            ForEach(Array(screen.agents.prefix(5).enumerated()), id: \.element.id) { i, line in
                Button { actions.openAgent(line.id) } label: {
                    HStack(spacing: 12) {
                        CharacterView(id: line.id, kind: line.agentKind, state: line.state, size: 34)
                        VStack(alignment: .leading, spacing: 0) {
                            Text(line.name).font(.system(size: 17)).foregroundStyle(Color.madFg).lineLimit(1)
                            if !line.sub.isEmpty { Text(line.sub).font(.system(size: 14)).foregroundStyle(Color.madFg3).lineLimit(1) }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        Text(line.number).font(.system(size: 17)).monospacedDigit().foregroundStyle(Color.madFg2).lineLimit(1).fixedSize()
                    }
                    .padding(.horizontal, 18)
                    .padding(.vertical, 10)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .overlay(alignment: .top) {
                    if i > 0 { Rectangle().fill(Color.madLine).frame(height: 1).padding(.leading, 64).padding(.trailing, 18) }
                }
            }
        }
    }

    private func budget(_ b: UsageBudget) -> some View {
        PhoneCard(L("mad 自己的 agent 预算")) {
            meter(label: b.note, value: "", part: b.part, hot: b.hot || b.closed)
            if b.closed {
                Text(L("已用完，agent 在等")).font(.system(size: 15)).foregroundStyle(Color.madWait).padding(.horizontal, 18).padding(.bottom, 6)
            }
            if !b.open {
                Button(action: actions.liftToday) {
                    Text(L("今天不限")).font(.system(size: 17, weight: .semibold)).foregroundStyle(Color.madFg)
                        .frame(maxWidth: .infinity, minHeight: 46)
                        .background(Color.madPaper, in: Capsule())
                        .overlay(Capsule().strokeBorder(Color.madLine2, lineWidth: 1))
                        .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .padding(.horizontal, 18).padding(.top, 4).padding(.bottom, 12)
            }
        }
    }

    /// A label and its value over an 8pt bar: green, amber when hot.
    private func meter(label: String, value: String, part: Double, hot: Bool) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(label).font(.system(size: 17)).foregroundStyle(Color.madFg).lineLimit(1)
                Spacer(minLength: 8)
                if !value.isEmpty {
                    Text(value).font(.system(size: 15)).monospacedDigit().foregroundStyle(Color.madFg3).lineLimit(1)
                }
            }
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Capsule().fill(Color.madRaise2)
                    Capsule().fill(hot ? Color.madWait : Color.madWork).frame(width: g.size.width * min(1, max(0, part)))
                }
            }
            .frame(height: 8)
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 10)
        .accessibilityElement(children: .combine)
    }
}
#endif
