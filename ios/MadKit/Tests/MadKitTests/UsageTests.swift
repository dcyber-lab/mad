import XCTest
@testable import MadKit

final class UsageTests: XCTestCase {
    private var cal: Calendar {
        var c = Calendar(identifier: .gregorian)
        c.timeZone = TimeZone(identifier: "UTC")!
        return c
    }
    private var words: UsageWording { UsageWording(calendar: cal, locale: Locale(identifier: "en_US_POSIX")) }
    /// Friday 2026-10-30 16:30 UTC.
    private let now = ISO8601DateFormatter().date(from: "2026-10-30T16:30:00Z")!

    private func window(after seconds: TimeInterval, used: Double = 40) -> UsageWindow {
        UsageWindow(used: used, resetAt: now.addingTimeInterval(seconds))
    }

    func testResetWithinADayIsHoursMinutesAndClock() {
        XCTAssertEqual(words.reset(window(after: 2 * 3600 + 10 * 60), now: now), "resets in 2h 10m, at 18:40")
        XCTAssertEqual(words.reset(window(after: 25 * 60), now: now), "resets in 25m, at 16:55")
        XCTAssertEqual(words.reset(window(after: 3600), now: now), "resets in 1h, at 17:30")
        XCTAssertEqual(words.reset(window(after: 23 * 3600 + 59 * 60), now: now), "resets in 23h 59m, at 16:29")
    }

    func testResetBeyondADayIsWeekdayAndClock() {
        XCTAssertEqual(words.reset(window(after: 24 * 3600), now: now), "resets Sat 16:30")
        let monday9 = ISO8601DateFormatter().date(from: "2026-11-02T09:00:00Z")!
        XCTAssertEqual(words.reset(UsageWindow(used: 71, resetAt: monday9), now: now), "resets Mon 09:00")
    }

    func testNoResetOrAPastOneIsNil() {
        XCTAssertNil(words.reset(UsageWindow(used: 10), now: now))
        XCTAssertNil(words.reset(window(after: -60), now: now))
        XCTAssertNil(words.reset(window(after: 0), now: now))
    }

    func testLeftText() {
        XCTAssertEqual(UsageWindow(used: 42.4).leftText, "58% left")
        XCTAssertEqual(UsageWindow(used: 0).leftText, "100% left")
        XCTAssertEqual(UsageWindow(used: 100).leftText, "0% left")
        XCTAssertEqual(UsageWindow(used: 130).leftText, "over the limit")
    }

    func testReportedTime() {
        let u = Usage(kind: "claude", fiveHour: UsageWindow(), sevenDay: UsageWindow(),
                      reportedAt: now.addingTimeInterval(-4 * 60))
        XCTAssertEqual(words.reported(u, now: now), "Reported 16:26")
        let old = Usage(kind: "claude", fiveHour: UsageWindow(), sevenDay: UsageWindow(),
                        reportedAt: now.addingTimeInterval(-26 * 3600))
        XCTAssertEqual(words.reported(old, now: now), "Reported Thu 14:30")
        XCTAssertNil(words.reported(Usage(kind: "x", fiveHour: UsageWindow(), sevenDay: UsageWindow()), now: now))
    }

    func testDecodesQuotaTolerantly() throws {
        let json = """
        {"quota": {
          "codex": {"five_hour": {"used": 9, "reset_at": "2026-10-30T18:40:00.5Z"},
                    "seven_day": {"used": 23.5, "reset_at": "2026-11-02T09:00:00Z"}, "at": "2026-10-30T16:26:00Z", "future": 1},
          "claude": {"five_hour": {"used": 42, "reset_at": "2026-10-30T18:40:00Z"},
                     "seven_day": {"used": 0, "reset_at": "0001-01-01T00:00:00Z"}, "at": "2026-10-30T16:20:00Z"},
          "pi": {"five_hour": "broken"},
          "zed": 5
        }}
        """
        let s = try JSONDecoder().decode(Deck.self, from: Data(json.utf8))
        XCTAssertEqual(s.usage.map(\.kind), ["claude", "codex", "pi"], "claude, then codex, then the rest; an unreadable entry is left out")
        let claude = s.usage[0]
        XCTAssertEqual(claude.title, "Claude")
        XCTAssertEqual(claude.fiveHour.used, 42)
        XCTAssertNotNil(claude.fiveHour.resetAt)
        XCTAssertNil(claude.sevenDay.resetAt, "Go's zero time is no reset time")
        XCTAssertFalse(claude.sevenDay.known, "a window the Mac zeroed")
        XCTAssertTrue(claude.fiveHour.known)
        XCTAssertNotNil(claude.reportedAt)
        XCTAssertEqual(s.usage[1].sevenDay.used, 23.5)
        XCTAssertFalse(s.usage[2].known)
    }

    func testNoQuotaIsEmpty() throws {
        XCTAssertEqual(try JSONDecoder().decode(Deck.self, from: Data("{}".utf8)).usage, [])
        XCTAssertEqual(try JSONDecoder().decode(Deck.self, from: Data(#"{"quota": {}}"#.utf8)).usage, [])
    }
}
