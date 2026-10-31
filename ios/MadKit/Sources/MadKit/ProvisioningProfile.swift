import Foundation

/// The provisioning profile an installed build carries (`embedded.mobileprovision`):
/// a free Apple ID's lasts 7 days, after which the app stops opening.
public enum ProvisioningProfile {
    /// The profile's ExpirationDate. The file is a signed (CMS) wrapper around
    /// a plist, so the plist is cut out of it by its tags. nil when it holds none.
    public static func expirationDate(in data: Data) -> Date? {
        guard let start = data.range(of: Data("<?xml".utf8)),
              let end = data.range(of: Data("</plist>".utf8), in: start.lowerBound..<data.endIndex) else { return nil }
        let plist = data.subdata(in: start.lowerBound..<end.upperBound)
        guard let object = try? PropertyListSerialization.propertyList(from: plist, options: [], format: nil),
              let dict = object as? [String: Any] else { return nil }
        return dict["ExpirationDate"] as? Date
    }

    /// Whole days left, rounded up: 1 for anything under a day, 0 or less once it ran out.
    public static func daysLeft(until expiration: Date, now: Date = Date()) -> Int {
        let remaining = expiration.timeIntervalSince(now)
        return remaining <= 0 ? 0 : Int((remaining / 86_400).rounded(.up))
    }

    /// How a build stands when it is about to run out, or has.
    public enum Expiry: Equatable, Sendable {
        case expired
        /// Whole days left, rounded up: 1 for anything under a day.
        case daysLeft(Int)

        public var english: String {
            switch self {
            case .expired: return "This build has expired: run make ios-device on the Mac to renew."
            case .daysLeft(let n):
                return "This build expires in \(n) \(n == 1 ? "day" : "days"): run make ios-device on the Mac to renew."
            }
        }

        public var chinese: String {
            switch self {
            case .expired: return "这个版本已失效：在 Mac 上执行 make ios-device 续期"
            case .daysLeft(let n): return "这个版本 \(n) 天后失效：在 Mac 上执行 make ios-device 续期"
            }
        }
    }

    /// The build's standing: nil while there is more than `warnWithinDays` left.
    public static func expiry(expiration: Date, now: Date = Date(), warnWithinDays: Int = 2) -> Expiry? {
        if expiration.timeIntervalSince(now) <= 0 { return .expired }
        let n = daysLeft(until: expiration, now: now)
        return n <= warnWithinDays ? .daysLeft(n) : nil
    }

    /// What to tell the person when the build is about to run out (2 days or
    /// fewer), or has; nil while there is time.
    public static func expiryNotice(expiration: Date, now: Date = Date(), warnWithinDays: Int = 2) -> String? {
        expiry(expiration: expiration, now: now, warnWithinDays: warnWithinDays)?.english
    }
}

/// A notice the person put away stays away for the rest of that day.
public enum ExpiryDismissal {
    /// The day a dismissal was made on, as "2026-10-31" in the calendar's own time zone.
    public static func dayKey(_ now: Date, calendar: Calendar = .current) -> String {
        let c = calendar.dateComponents([.year, .month, .day], from: now)
        return String(format: "%04d-%02d-%02d", c.year ?? 0, c.month ?? 0, c.day ?? 0)
    }

    /// Whether to keep the notice away: it was dismissed today.
    public static func isDismissed(storedDay: String?, now: Date, calendar: Calendar = .current) -> Bool {
        storedDay == dayKey(now, calendar: calendar)
    }
}
