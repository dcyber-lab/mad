import Foundation
import MadKit
import MadUI

/// The deck onto the Usage screen: each subscription's two windows, what the agents have cost by project and the
/// agents that cost the most, and mad's own budget for the day.
public enum UsageMapping {
    public static func screen(from deck: Deck, now: Date = Date(), calendar: Calendar = .current) -> UsageScreen {
        var quotas: [UsageQuota] = []
        for u in deck.usage where u.known {
            for (window, name) in [(u.fiveHour, L("5 小时")), (u.sevenDay, L("每周"))] where window.known {
                quotas.append(UsageQuota(title: u.title, window: name, percent: max(0, Int(window.used.rounded())),
                                         reset: reset(window, now: now, calendar: calendar)))
            }
        }
        // by project, the most first
        let costs: [(cost: Double, project: Project)] = deck.projects.map { p in (p.agents.reduce(0.0) { $0 + $1.cost }, p) }
        let projects: [UsageLine] = costs.filter { $0.cost > 0 }.sorted { $0.cost > $1.cost }.map { c in
            let n = c.project.agents.count
            return UsageLine(id: c.project.id, name: c.project.name, sub: n == 1 ? L("1 个 agent") : Lf("%lld 个 agent", n),
                             mono: c.project.branch ?? "", number: MadWords.money(c.cost))
        }
        let agents = deck.agents.filter { $0.cost > 0 }.sorted { $0.cost > $1.cost }.prefix(5).map { a in
            let project = deck.projects.first { $0.agents.contains { $0.id == a.id } }?.name ?? ""
            return UsageLine(id: a.id, name: a.label, sub: [project, a.kind].filter { !$0.isEmpty }.joined(separator: " · "),
                             number: MadWords.money(a.cost), agent: true, agentKind: a.kind, state: CharacterState(status: a.state))
        }
        let total = deck.agents.reduce(0.0) { $0 + $1.cost }
        return UsageScreen(quotas: quotas, total: total > 0 ? MadWords.money(total) : "", projects: projects,
                           agents: Array(agents), budget: budget(deck.spend))
    }

    /// mad's own budget: "每天 $20 · 已用 $6.82" and the part spent; nil when the server says nothing of it.
    static func budget(_ s: Spend?) -> UsageBudget? {
        guard let s, !s.day.isEmpty else { return nil }
        let used = Lf("已用 %@", MadWords.money(s.usd))
        let note = s.budgetUSD > 0 ? Lf("每天 %@", MadWords.money(s.budgetUSD)) + " · " + used : used
        return UsageBudget(note: s.openToday ? L("今天不限") + " · " + used : note, part: s.part, closed: s.closed, open: s.openToday)
    }

    /// When a window resets: "2:10 后重置" within a day, "周四 09:00 重置" later; empty when not known or past.
    static func reset(_ w: UsageWindow, now: Date, calendar: Calendar) -> String {
        guard let at = w.resetAt, at > now else { return "" }
        let minutes = Int((at.timeIntervalSince(now) / 60).rounded(.up))
        if minutes < 24 * 60 {
            return Lf("%lld:%02lld 后重置", minutes / 60, minutes % 60)
        }
        let f = DateFormatter()
        f.calendar = calendar
        f.timeZone = calendar.timeZone
        f.locale = Locale(identifier: MadLocale.isEnglish(bundle: .module) ? "en_US" : "zh_CN")
        f.dateFormat = "EEE HH:mm"
        return Lf("%@ 重置", f.string(from: at))
    }
}
