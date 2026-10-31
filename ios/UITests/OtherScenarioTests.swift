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
    func testRedDotRowsComeFirst() {
        launch()
        waitForHome()
        settle(1)
        shot("many channels")
        let rows = web.buttons.allElementsBoundByIndex.filter { $0.frame.height > 40 && $0.frame.minY > 150 }
            .sorted { $0.frame.minY < $1.frame.minY }
        XCTAssertGreaterThanOrEqual(rows.count, 6, "too few channel rows to look at")
        let dots = element("needs you").allElementsBoundByIndex
        let hasDot = rows.map { row in dots.contains { row.frame.intersects($0.frame) && $0.frame.width < 24 } }
        var seenPlain = false
        for (i, dot) in hasDot.enumerated() {
            if !dot { seenPlain = true }
            XCTAssertFalse(dot && seenPlain, "row \(i) (“\(rows[i].label)”) has a red dot but comes after a row without one")
        }
    }
}

/// The usage page (`usage` scenario): 42%/71%, 104% (over), and a window with no reset time.
final class UsageTests: MadUITestCase {
    func testUsageShowsPercentsAndNoNegativeLeftOrZeroTime() {
        launch()
        waitForHome()
        tap("更多")
        tap("用量")
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
