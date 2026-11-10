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

    func testAPinnedWindowKeepsItsFirstRowWhenMessagesArrive() {
        var w = HeldWindow()
        var all = ids(260)
        w.pin(in: all, atEnd: false) // a reader a little way up, the window never touched
        let first = all[w.start(in: all)]
        XCTAssertEqual(first, "r220")
        all.append("r260")
        all.append("r261")
        XCTAssertEqual(all[w.start(in: all)], first, "an untouched window slid and dropped the row at its top")
    }

    func testAMessageThatArrivesRightAfterALoadKeepsTheLoadedFirstRow() {
        var w = HeldWindow()
        var all = ids(260)
        w.pin(in: all, atEnd: false)
        w.earlier(in: all) // a load: 180
        all.append("r260") // and a message in the same moment
        XCTAssertEqual(w.start(in: all), 180, "the load and the message disagree about the first row")
    }

    func testAShortChannelIsPinnedWholeAndStaysWholeAsItGrows() {
        var w = HeldWindow()
        var all = ids(10)
        w.pin(in: all, atEnd: true)
        all += ids(20, from: 10)
        XCTAssertEqual(w.start(in: all), 0)
    }

    func testAReaderAtTheEndGetsTheLastRowsAgainOnceTheStackHasGrownPastTwiceThat() {
        var w = HeldWindow()
        var all = ids(40)
        w.pin(in: all, atEnd: true)
        all += ids(41, from: 40) // 81 rows held
        w.pin(in: all, atEnd: true)
        XCTAssertEqual(w.start(in: all), 41, "the stack was left growing at the end")
        var up = HeldWindow()
        up.pin(in: ids(40), atEnd: true)
        up.pin(in: all, atEnd: false) // the same, but the reader is up in the history
        XCTAssertEqual(up.start(in: all), 0, "a reader away from the end lost the rows being read")
    }

    func testAPinnedFirstRowThatIsGoneIsPinnedAgain() {
        var w = HeldWindow()
        var all = ids(260)
        w.pin(in: all, atEnd: false)
        all.removeFirst(230)
        w.pin(in: all, atEnd: false)
        XCTAssertEqual(w.start(in: all), 0)
        XCTAssertEqual(w.firstID, "r230")
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
