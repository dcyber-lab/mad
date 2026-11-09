#if os(macOS)
import AppKit
import SwiftUI
import XCTest
@testable import MadUI

@MainActor
final class TurningArcTests: XCTestCase {
    /// Lays `view` out in an offscreen window and returns the arc layer under it.
    private func arc<V: View>(_ view: V, size: CGFloat) throws -> CAShapeLayer {
        let host = NSHostingView(rootView: view)
        host.frame = NSRect(x: 0, y: 0, width: size, height: size)
        let window = NSWindow(contentRect: host.frame, styleMask: .borderless, backing: .buffered, defer: false)
        window.contentView = host
        host.layoutSubtreeIfNeeded()
        host.displayIfNeeded()
        func find(_ v: NSView) -> CAShapeLayer? {
            if let l = v.layer?.sublayers?.compactMap({ $0 as? CAShapeLayer }).first { return l }
            return v.subviews.lazy.compactMap(find).first
        }
        return try XCTUnwrap(find(host), "no arc layer under the ring")
    }

    func testTheArcIsALayerTheRenderServerTurns() throws {
        let l = try arc(StatusRing(state: .working, size: 40), size: 40)
        XCTAssertEqual(l.bounds.size, CGSize(width: 40, height: 40))
        XCTAssertEqual(l.lineWidth, 2, accuracy: 1e-6, "5 of 100")
        XCTAssertEqual(l.strokeEnd, 0.265, accuracy: 1e-6)
        let spin = try XCTUnwrap(l.animation(forKey: "mad.spin") as? CABasicAnimation)
        XCTAssertEqual(spin.keyPath, "transform.rotation.z")
        XCTAssertEqual(spin.duration, 1.4, accuracy: 1e-6)
        XCTAssertFalse(spin.isRemovedOnCompletion, "it must still turn after the view leaves the window")
        XCTAssertNotNil(l.strokeColor)
    }

    func testUnderReduceMotionTheArcStaysStill() throws {
        let l = try arc(ArcLayerView(size: 40, still: true), size: 40)
        XCTAssertNil(l.animation(forKey: "mad.spin"))
    }
}
#endif
