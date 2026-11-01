import XCTest

/// The empty state, against the fixture server's `empty` scenario.
final class EmptyTests: MadUITestCase {
    func testTheEmptyStateShows() {
        launch()
        waitForHome()
        XCTAssertTrue(elements(containing: "No projects yet").firstMatch.waitForExistence(timeout: 5)
                      || elements(containing: "还没有").firstMatch.exists, "no empty state")
        shot("empty state")
    }
}

/// Twelve channels: rows with a red dot first, then newest first (`many-channels`).
final class ManyChannelsTests: MadUITestCase {
    // By the fixture's times: the three with a dot (charlie -20m, foxtrot -40m, india -6h), then the rest, newest first.
    private let order = ["charlie", "foxtrot", "india", "alpha", "hotel", "juliet", "bravo", "golf", "delta", "kilo", "lima", "echo"]

    func testRedDotRowsComeFirstThenNewest() {
        launch()
        waitForHome()
        // Charlie, foxtrot and india have a red dot; the rest follow by time.
        let rows = order.map { web.buttons[$0] }
        wait("not all twelve channels are listed") { rows.allSatisfy { $0.exists } }
        shot("many channels")
        for (name, row) in zip(order, rows) { XCTAssertTrue(row.exists, "no row named \(name)") }
        let ys = rows.map { $0.frame.minY }
        XCTAssertEqual(ys, ys.sorted(), "the order is not \(order.joined(separator: ", ")): \(zip(order, ys).map { "\($0) \(Int($1))" })")
        func dots(_ row: XCUIElement) -> Int {
            element("needs you").allElementsBoundByIndex.filter { row.frame.contains(CGPoint(x: $0.frame.midX, y: $0.frame.midY)) }.count
        }
        for name in ["charlie", "foxtrot", "india"] {
            XCTAssertGreaterThanOrEqual(dots(web.buttons[name]), 1, "the \(name) row has no red dot")
        }
        for name in order.dropFirst(3) {
            XCTAssertEqual(dots(web.buttons[name]), 0, "the \(name) row has a red dot it should not")
        }
    }
}

/// The usage page (`usage` scenario): 42%/71%, 104% (over), and a window with no reset time.
final class UsageTests: MadUITestCase {
    func testUsageShowsPercentsAndNoNegativeLeftOrZeroTime() {
        launch()
        waitForHome()
        tap("更多")
        // the item carries a red dot when the day's budget is spent, which the label may say too
        let item = elements(containing: "用量").firstMatch
        XCTAssertTrue(item.waitForExistence(timeout: 10), "no “用量” to tap")
        item.tap()
        XCTAssertTrue(elements(containing: "42%").firstMatch.waitForExistence(timeout: 10), "no 42%")
        for p in ["71%", "104%"] { XCTAssertTrue(elements(containing: p).firstMatch.exists, "no \(p)") }
        shot("usage")
        let negative = web.descendants(matching: .any).matching(NSPredicate(format: "label MATCHES %@", ".*-\\d+%.*")).count
        XCTAssertEqual(negative, 0, "a negative percent shows")
        for artifact in ["0001", "Jan 1, 1", "1/1/1", "1970"] {
            XCTAssertFalse(elements(containing: artifact).firstMatch.exists, "a zero time shows as “\(artifact)”")
        }
    }
}
