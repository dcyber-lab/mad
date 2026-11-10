import XCTest

/// A channel of 260 messages (scenario `channel-open-long`, native): it opens on its last rows, and the older ones come in by
/// themselves as the person scrolls up, with the rows being read staying where they are.
final class ChannelWindowTests: MadUITestCase {
    /// The messages on screen: text label and where its top is. Read in one snapshot.
    private func visible() -> [String: CGFloat] {
        guard let tree = try? app.windows.firstMatch.snapshot() else { return [:] }
        var found: [String: CGFloat] = [:]
        func walk(_ s: XCUIElementSnapshot) {
            let f = s.frame
            if s.elementType == .staticText, s.label.count >= 20, f.height > 0, f.maxY > 130, f.minY < screenFrame.maxY - 90 { found[s.label] = f.minY }
            s.children.forEach(walk)
        }
        walk(tree)
        return found
    }

    override func setUp() {
        super.setUp()
        executionTimeAllowance = 900
    }

    func testOpensAtTheEndAndOlderRowsLoadWithoutMovingTheOnesBeingRead() {
        launch(native: true, extra: ["-nativeOpen", "fold"])
        wait("the channel's messages did not come", timeout: 40) { self.visible().count >= 2 }
        sleep(2)
        XCTAssertFalse(element("到最新").firstMatch.exists, "the channel opened with the way-down button: not at its end")
        shot("opened at the end")
        let from = app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.22))
        let to = app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.78))
        var moves: [CGFloat] = []
        var before = visible()
        // 36 slow drags of the finger (about 18000pt): more than the 40 rows held, about 10000pt, so the held rows end at least once if nothing loads
        for step in 1...36 {
            from.press(forDuration: 0.05, thenDragTo: to, withVelocity: .slow, thenHoldForDuration: 0.4)
            Thread.sleep(forTimeInterval: 0.5)
            let after = visible()
            let common = Set(before.keys).intersection(after.keys)
            if common.isEmpty {
                func dump(_ m: [String: CGFloat]) -> String { m.sorted { $0.value < $1.value }.map { "\(Int($0.value)) \($0.key.prefix(12))" }.joined(separator: " | ") }
                shot("step \(step) no common")
                XCTFail("step \(step): no message is on screen both before and after. before: \(dump(before)). after: \(dump(after)). moves so far: \(moves.map { Int($0) })")
                return
            }
            let deltas = common.map { after[$0]! - before[$0]! }
            if let lo = deltas.min(), let hi = deltas.max() {
                XCTAssertLessThanOrEqual(hi - lo, 2, "step \(step): the rows on screen moved against each other by \(hi - lo)pt")
                moves.append((lo + hi) / 2)
            }
            before = after
            if step == 8 { shot("scrolling up, rows older than the first 40 held") }
        }
        // every drag moved the list by about the same: a load that jumped the list would be a step off the rest
        let sorted = moves.sorted()
        let median = sorted[sorted.count / 2]
        XCTAssertGreaterThan(median, 100, "the drags did not scroll the list")
        for (i, m) in moves.enumerated() {
            XCTAssertEqual(m, median, accuracy: 2, "step \(i + 1) moved the list \(m)pt, the others \(median)pt: the list jumped (a load) or stopped (nothing loaded)")
        }
        shot("further up")
    }

    /// Waits until two snapshots 0.3s apart show the same rows in the same places: the list is at rest.
    private func waitForRest(_ step: Int) -> [String: CGFloat] {
        var last = visible()
        for _ in 0..<40 {
            Thread.sleep(forTimeInterval: 0.3)
            let now = visible()
            if now == last { return now }
            last = now
        }
        XCTFail("flick \(step): the list never came to rest")
        return last
    }

    /// Flicks up the history, as a person reads back: each one fast, the list comes to rest, and what loads at rest moves
    /// nothing. The held top must stay far above, so no flick runs into it and stops dead.
    func testFlicksUpTheHistoryNeverRunIntoTheHeldTop() {
        launch(native: true, extra: ["-nativeOpen", "fold"])
        wait("the channel's messages did not come", timeout: 40) { self.visible().count >= 2 }
        sleep(2)
        let heldTop = app.descendants(matching: .any)["channel.heldTop"].firstMatch
        var previous = visible()
        var moved = 0
        for flick in 1...10 {
            app.swipeDown(velocity: .fast)
            let atRest = waitForRest(flick)
            XCTAssertTrue(heldTop.exists, "flick \(flick): the held top is not in the accessibility tree, so the look-ahead is not measured")
            if heldTop.exists {
                // the held top is more than a screen above the list's top: nothing to run into (the load took more when it came near)
                XCTAssertLessThan(heldTop.frame.minY, -screenFrame.height, "flick \(flick): the held top is only \(Int(heldTop.frame.minY))pt from the list's top, within a flick's reach")
            }
            Thread.sleep(forTimeInterval: 1)
            let later = visible()
            let common = Set(atRest.keys).intersection(later.keys)
            XCTAssertFalse(common.isEmpty, "flick \(flick): the rows on screen at rest are not the ones a second later (a load moved them)")
            for key in common { XCTAssertEqual(later[key]!, atRest[key]!, accuracy: 2, "flick \(flick): a load at rest moved a row") }
            if atRest != previous { moved += 1 }
            previous = later
            if flick == 5 { shot("after five flicks") }
        }
        XCTAssertEqual(moved, 10, "a flick moved nothing: the list was stopped at the held top")
    }
}
