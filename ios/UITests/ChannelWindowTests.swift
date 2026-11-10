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
}
