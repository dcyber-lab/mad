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
        w.pin(in: all) // a reader a little way up, the window never touched
        let first = all[w.start(in: all)]
        XCTAssertEqual(first, "r220")
        all.append("r260")
        all.append("r261")
        XCTAssertEqual(all[w.start(in: all)], first, "an untouched window slid and dropped the row at its top")
    }

    func testAMessageThatArrivesRightAfterALoadKeepsTheLoadedFirstRow() {
        var w = HeldWindow()
        var all = ids(260)
        w.pin(in: all)
        w.earlier(in: all) // a load: 180
        all.append("r260") // and a message in the same moment
        XCTAssertEqual(w.start(in: all), 180, "the load and the message disagree about the first row")
    }

    func testAShortChannelIsPinnedWholeAndStaysWholeAsItGrows() {
        var w = HeldWindow()
        var all = ids(10)
        w.pin(in: all)
        all += ids(20, from: 10)
        XCTAssertEqual(w.start(in: all), 0)
    }

    func testATrimNeedsTheFlagAndAWindowPastTwiceTheFirstRows() {
        var all = ids(40)
        var w = HeldWindow()
        w.pin(in: all)
        all += ids(41, from: 40) // 81 rows held, 41 came
        XCTAssertFalse(w.pin(in: all, trim: true), "cut after 41 messages: it waits for 80")
        all += ids(40, from: 81)
        XCTAssertFalse(w.pin(in: all, trim: false), "cut without being asked")
        XCTAssertEqual(w.start(in: all), 0)
        XCTAssertTrue(w.pin(in: all, trim: true))
        XCTAssertEqual(w.start(in: all), 81)
        var small = HeldWindow()
        small.pin(in: ids(60))
        XCTAssertFalse(small.pin(in: ids(60), trim: true), "a window of 60 rows is cut to 40")
    }

    func testTheLookIsLoadHoldOrTrimWithAGapBetween() {
        let v: CGFloat = 800
        XCTAssertEqual(HeldWindow.look(topY: -3000, viewport: v), .load)
        XCTAssertEqual(HeldWindow.look(topY: -5 * v, viewport: v), .hold, "the load stops at five screens")
        XCTAssertEqual(HeldWindow.look(topY: -7 * v, viewport: v), .hold)
        XCTAssertEqual(HeldWindow.look(topY: -11 * v, viewport: v), .trim)
    }

    /// A reader at the end while messages keep coming, at rows of every height: the held top asks for loads and cuts as the look
    /// says, at rest. However short or tall the rows, the stack changes rarely, not on every message.
    func testShortOrTallRowsDoNotMakeTheWindowLoadAndCutOnEveryMessage() {
        let v: CGFloat = 800
        for h: CGFloat in [20, 50, 100, 200, 300] {
            var w = HeldWindow()
            var all = ids(40)
            w.pin(in: all)
            var changes = 0
            func settle() {
                // loads chain at rest until the held top is five screens up (or everything is held)
                while true {
                    let held = all.count - w.start(in: all)
                    let topY = -(CGFloat(held) * h - v)
                    guard !w.holdsAll(all), HeldWindow.look(topY: topY, viewport: v) == .load else { break }
                    w.earlier(in: all); changes += 1
                }
            }
            settle()
            changes = 0
            for n in 0..<400 {
                all.append("m\(n)")
                let held = all.count - w.start(in: all)
                let topY = -(CGFloat(held) * h - v)
                if w.pin(in: all, trim: HeldWindow.look(topY: topY, viewport: v) == .trim) { changes += 1 }
                settle()
            }
            XCTAssertLessThanOrEqual(changes, 400 / 20, "rows of \(Int(h))pt: \(changes) loads and cuts in 400 messages")
        }
    }

    func testAPinnedFirstRowThatIsGoneIsPinnedAgain() {
        var w = HeldWindow()
        var all = ids(260)
        w.pin(in: all)
        all.removeFirst(230)
        w.pin(in: all)
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
