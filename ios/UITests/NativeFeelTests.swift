import XCTest

/// The deck in the app is not a web page: a double tap or a pinch does not zoom it.
/// A zoom would move and scale everything on the page, so the header's buttons are
/// where they were. Against the `empty` scenario, whose page has no rows for the
/// fingers to land on (a pinch over rows would be taps, and open one).
final class NativeFeelTests: MadUITestCase {
    override func setUp() {
        super.setUp()
        launch()
        waitForHome()
    }

    private func frames() -> [CGRect] {
        ["切换 Mac", "搜索", "新建"].map { element($0).firstMatch.frame }
    }

    func testDoubleTapDoesNotZoom() {
        let before = frames()
        XCTAssertFalse(before.contains(.zero), "the header's buttons are not all there: \(before)")
        shot("before the double tap")
        web.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.6)).doubleTap()
        Thread.sleep(forTimeInterval: 1.0) // a zoom animates
        shot("after the double tap")
        XCTAssertEqual(frames(), before, "a double tap moved or scaled the page: it zoomed")
    }

    func testPinchDoesNotZoom() {
        let before = frames()
        XCTAssertFalse(before.contains(.zero), "the header's buttons are not all there: \(before)")
        web.pinch(withScale: 2.5, velocity: 2)
        Thread.sleep(forTimeInterval: 1.0)
        shot("after the pinch")
        XCTAssertEqual(frames(), before, "a pinch moved or scaled the page: it zoomed")
    }
}
