import XCTest
@testable import MadUI

final class HeldWindowTests: XCTestCase {
    private func ids(_ n: Int, from: Int = 0) -> [String] { (from..<(from + n)).map { "r\($0)" } }

    func testAChannelOpensOnItsLastRows() {
        let w = HeldWindow()
        XCTAssertEqual(w.start(in: ids(260)), 220)
        XCTAssertEqual(w.start(in: ids(30)), 0, "a short channel is held whole")
    }

    func testAMessageThatArrivesWhileScrolledUpKeepsTheFirstRow() {
        var w = HeldWindow()
        var all = ids(260)
        w.earlier(in: all) // the person scrolled up: 180
        let first = all[w.start(in: all)]
        all.append("r260")
        all.append("r261")
        XCTAssertEqual(all[w.start(in: all)], first, "the window slid and dropped the row being read")
    }

    func testAnUntouchedWindowSlidesWithTheEnd() {
        let w = HeldWindow()
        XCTAssertEqual(w.start(in: ids(261)), 221, "before any scroll the window is the last rows")
    }

    func testEarlierMovesTheFirstRowBackByMoreAndStopsAtTheTop() {
        var w = HeldWindow()
        let all = ids(100)
        w.earlier(in: all)
        XCTAssertEqual(w.start(in: all), 20)
        XCTAssertFalse(w.holdsAll(all))
        w.earlier(in: all)
        XCTAssertEqual(w.start(in: all), 0)
        XCTAssertTrue(w.holdsAll(all))
        w.earlier(in: all)
        XCTAssertEqual(w.start(in: all), 0)
    }

    func testAFirstRowThatIsGoneFallsBackToTheLastRows() {
        var w = HeldWindow()
        var all = ids(260)
        w.earlier(in: all)
        all.removeFirst(190) // the row was dropped from the channel (the device keeps the newest)
        XCTAssertEqual(w.start(in: all), 30, "a window whose first row is gone is the last rows again")
    }
}
