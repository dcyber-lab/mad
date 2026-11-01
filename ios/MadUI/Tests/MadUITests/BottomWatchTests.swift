import XCTest
@testable import MadUI

final class BottomWatchTests: XCTestCase {
    private let t0 = Date(timeIntervalSince1970: 1_000)

    func testAMarkerThatLeavesWithNoFollowIsLeftAtOnce() {
        var w = BottomWatch()
        XCTAssertEqual(w.disappeared(at: t0), .now)
    }

    func testAMarkerThatLeavesRightAfterAFollowIsJudgedWhenTheWindowEnds() {
        var w = BottomWatch()
        w.followed(at: t0)
        guard case .recheck(let after) = w.disappeared(at: t0.addingTimeInterval(0.2)) else { return XCTFail("not deferred") }
        XCTAssertEqual(after, 0.3, accuracy: 1e-3)
        XCTAssertTrue(w.stillLeft, "the person scrolled up inside the window: it is still out at the recheck")
    }

    func testTheMarkerBackByTheRecheckMeansTheListFollowedAndNobodyScrolled() {
        var w = BottomWatch()
        w.followed(at: t0)
        _ = w.disappeared(at: t0.addingTimeInterval(0.05))
        w.appeared() // the follow brought it back
        XCTAssertFalse(w.stillLeft)
    }

    func testALeaveAfterTheWindowCountsAtOnce() {
        var w = BottomWatch()
        w.followed(at: t0)
        XCTAssertEqual(w.disappeared(at: t0.addingTimeInterval(BottomWatch.window + 0.01)), .now)
    }

    func testAnotherFollowOpensANewWindow() {
        var w = BottomWatch()
        w.followed(at: t0)
        w.followed(at: t0.addingTimeInterval(2))
        guard case .recheck(let after) = w.disappeared(at: t0.addingTimeInterval(2.1)) else { return XCTFail("not deferred") }
        XCTAssertEqual(after, 0.4, accuracy: 1e-3)
    }

    func testAnEmptyBandUnderTheLastRowIsPastTheEnd() {
        XCTAssertTrue(BottomWatch.pastEnd(markerShown: true, markerFoot: 440, viewportHeight: 640))
        XCTAssertFalse(BottomWatch.pastEnd(markerShown: true, markerFoot: 632, viewportHeight: 640), "the marker's foot sits 10 above the edge when at the bottom")
        XCTAssertFalse(BottomWatch.pastEnd(markerShown: false, markerFoot: 440, viewportHeight: 640), "a marker out of sight is the person reading above")
        XCTAssertFalse(BottomWatch.pastEnd(markerShown: true, markerFoot: 440, viewportHeight: 0), "not measured yet")
    }

    func testAMarkerThatLeftWithAResizeIsPutBackAndOneThatLeftAloneIsNot() {
        var w = BottomWatch()
        w.resized(at: t0)
        _ = w.disappeared(at: t0.addingTimeInterval(0.2))
        XCTAssertTrue(w.leftWithResize, "it left a moment after the viewport changed")
        var v = BottomWatch()
        v.resized(at: t0)
        _ = v.disappeared(at: t0.addingTimeInterval(3))
        XCTAssertFalse(v.leftWithResize, "the person scrolled long after the resize")
        var back = BottomWatch()
        back.resized(at: t0)
        _ = back.disappeared(at: t0.addingTimeInterval(0.1))
        back.appeared()
        XCTAssertFalse(back.leftWithResize, "it came back")
    }
}
