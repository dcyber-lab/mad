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

    /// What to tell the person when the build is about to run out (2 days or
    /// fewer), or has; nil while there is time.
    public static func expiryNotice(expiration: Date, now: Date = Date(), warnWithinDays: Int = 2) -> String? {
        let n = daysLeft(until: expiration, now: now)
        if expiration.timeIntervalSince(now) <= 0 {
            return "This build has expired: run make ios-device on the Mac to renew."
        }
        guard n <= warnWithinDays else { return nil }
        return "This build expires in \(n) \(n == 1 ? "day" : "days"): run make ios-device on the Mac to renew."
    }
}
