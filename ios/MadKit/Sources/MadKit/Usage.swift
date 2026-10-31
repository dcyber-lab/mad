import Foundation

/// One metered window of a subscription (status.Window): percent used and
/// when it resets.
public struct UsageWindow: Equatable, Sendable {
    /// Percent, 0 to 100, or above over a spend limit.
    public var used: Double
    public var resetAt: Date?

    public init(used: Double = 0, resetAt: Date? = nil) { self.used = used; self.resetAt = resetAt }

    /// Whether the window was ever reported: the Mac zeroes one whose reset has passed.
    public var known: Bool { used > 0 || resetAt != nil }

    /// "58% left"; "over the limit" past 100.
    public var leftText: String {
        if used > 100 { return "over the limit" }
        return "\(max(0, 100 - Int(used.rounded())))% left"
    }
}

/// How much of one agent kind's subscription is used (status.Quota).
public struct Usage: Equatable, Sendable {
    public var kind: String
    public var fiveHour: UsageWindow
    public var sevenDay: UsageWindow
    /// When the Mac was told these numbers.
    public var reportedAt: Date?

    public init(kind: String, fiveHour: UsageWindow, sevenDay: UsageWindow, reportedAt: Date? = nil) {
        self.kind = kind; self.fiveHour = fiveHour; self.sevenDay = sevenDay; self.reportedAt = reportedAt
    }

    /// "Claude", "Codex".
    public var title: String { kind.prefix(1).uppercased() + kind.dropFirst() }
    public var known: Bool { fiveHour.known || sevenDay.known }
}

/// Tolerant decoding of one kind's entry of the snapshot's `quota`.
struct UsageEntry: Decodable {
    var fiveHour = UsageWindow()
    var sevenDay = UsageWindow()
    var at: Date?

    private struct Win: Decodable {
        var used: Double?
        var resetAt: String?
        enum CodingKeys: String, CodingKey { case used, resetAt = "reset_at" }
        var window: UsageWindow {
            UsageWindow(used: used ?? 0, resetAt: resetAt.flatMap(UsageEntry.date))
        }
    }

    enum CodingKeys: String, CodingKey { case fiveHour = "five_hour", sevenDay = "seven_day", at }

    init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        fiveHour = (try? c.decodeIfPresent(Win.self, forKey: .fiveHour))??.window ?? UsageWindow()
        sevenDay = (try? c.decodeIfPresent(Win.self, forKey: .sevenDay))??.window ?? UsageWindow()
        at = (try? c.decodeIfPresent(String.self, forKey: .at)).flatMap { $0 }.flatMap(Self.date)
    }

    /// Go writes a zero time as 0001-01-01 where a time is unknown.
    static func date(_ s: String) -> Date? {
        guard let d = InboxDates.parse(s), d.timeIntervalSince1970 > 946_684_800 else { return nil }
        return d
    }
}

/// Decodes a `quota` object, `{kind: entry}`, keeping each entry that reads.
struct UsageList: Decodable {
    var items: [Usage] = []

    private struct Key: CodingKey {
        var stringValue: String
        var intValue: Int? { nil }
        init?(stringValue: String) { self.stringValue = stringValue }
        init?(intValue: Int) { return nil }
    }

    init(from d: Decoder) throws {
        let c = try d.container(keyedBy: Key.self)
        for k in c.allKeys {
            guard let e = try? c.decode(UsageEntry.self, forKey: k) else { continue }
            items.append(Usage(kind: k.stringValue, fiveHour: e.fiveHour, sevenDay: e.sevenDay, reportedAt: e.at))
        }
        // claude and codex first, in that order; any other kind after, by name.
        let order = ["claude": 0, "codex": 1]
        items.sort { (order[$0.kind] ?? 2, $0.kind) < (order[$1.kind] ?? 2, $1.kind) }
    }
}

/// How times are put into words on the Usage page.
public struct UsageWording {
    public var calendar: Calendar
    public var locale: Locale

    public init(calendar: Calendar = .current, locale: Locale = .current) {
        self.calendar = calendar; self.locale = locale
    }

    private func format(_ date: Date, _ pattern: String) -> String {
        let f = DateFormatter()
        f.locale = locale
        f.calendar = calendar
        f.timeZone = calendar.timeZone
        f.dateFormat = pattern
        return f.string(from: date)
    }

    /// Within a day: "resets in 2h 10m, at 18:40". Later: "resets Mon 09:00".
    /// nil when it has no reset time, or that passed.
    public func reset(_ window: UsageWindow, now: Date = Date()) -> String? {
        guard let at = window.resetAt, at > now else { return nil }
        let minutes = Int((at.timeIntervalSince(now) / 60).rounded(.up))
        if minutes < 24 * 60 {
            let h = minutes / 60, m = minutes % 60
            let span = h == 0 ? "\(m)m" : (m == 0 ? "\(h)h" : "\(h)h \(m)m")
            return "resets in \(span), at \(format(at, "HH:mm"))"
        }
        return "resets \(format(at, "EEE HH:mm"))"
    }

    /// "Reported 14:52", or with the weekday when it was not today.
    public func reported(_ usage: Usage, now: Date = Date()) -> String? {
        guard let at = usage.reportedAt else { return nil }
        let today = calendar.isDate(at, inSameDayAs: now)
        return "Reported \(format(at, today ? "HH:mm" : "EEE HH:mm"))"
    }
}
