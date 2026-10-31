import SwiftUI
import XCTest
@testable import MadUI

final class LookTests: XCTestCase {
    /// What the web page's charLook picks: (key, hash, shape, face, color). Dumped from
    /// internal/desktop/web/app.js by `node ios/MadUI/scripts/dump-looks.js`.
    static let expected: [(String, UInt32, Int, Int, String)] = [
        ("architect", 172442964, 4, 1, "#3dcf9c"),
        ("builder", 3828680000, 0, 0, "#6f9bff"),
        ("reviewer", 829999642, 2, 4, "#b48cff"),
        ("claude-cc23", 2635099483, 3, 4, "#b48cff"),
        ("claude-3fe9", 2099053825, 1, 1, "#6f9bff"),
        ("codex 32d23a28", 2324632873, 1, 3, "#ffd34e"),
        ("a", 3826002220, 4, 4, "#ffd34e"),
        ("", 2166136261, 5, 4, "#6f9bff"),
        ("?", 973910158, 6, 1, "#ffb36b"),
        ("agent-0", 3830737203, 3, 3, "#5fd0e8"),
        ("agent-1", 3813959584, 0, 3, "#ff8fb3"),
        ("agent-2", 3864292441, 1, 4, "#b48cff"),
        ("agent-3", 3847514822, 6, 4, "#6f9bff"),
        ("agent-4", 3897847679, 7, 4, "#9be07f"),
        ("agent-5", 3881070060, 4, 4, "#ffd34e"),
        ("agent-6", 3931402917, 5, 0, "#ff8fb3"),
        ("agent-7", 3914625298, 2, 0, "#3dcf9c"),
        ("你好", 3619451238, 6, 0, "#ff8fb3"),
        ("😀 mad", 1747552053, 5, 0, "#5fd0e8"),
    ]

    func testHashShapeFaceColorMatchThePage() {
        for (key, hash, shape, face, color) in Self.expected {
            XCTAssertEqual(CharacterLook.hash(key), hash, "hash of \(key)")
            let look = CharacterLook(key: key)
            XCTAssertEqual(look.shape, shape, "shape of \(key)")
            XCTAssertEqual(look.face, face, "face of \(key)")
            XCTAssertEqual(look.colorHex, color, "color of \(key)")
        }
    }

    func testKeyFallsBackToKindThenQuestionMark() {
        XCTAssertEqual(CharacterLook(id: "", kind: "codex"), CharacterLook(key: "codex"))
        XCTAssertEqual(CharacterLook(id: "", kind: ""), CharacterLook(key: "?"))
        XCTAssertEqual(CharacterLook(id: "x", kind: "codex"), CharacterLook(key: "x"))
    }

    func testAllShapesAreNamedAsOnThePage() {
        XCTAssertEqual(CharacterLook.shapes.map(\.name), ["pebble", "toast", "bean", "drop", "cat", "hex", "puff", "sprout"])
        XCTAssertEqual(CharacterLook.faces.count, 5)
        XCTAssertEqual(CharacterLook.colorHexes.count, 8)
    }
}

/// The path's exact bounds (Path.boundingRect includes Bézier control points).
private func tight(_ p: Path) -> CGRect { p.cgPath.boundingBoxOfPath }

final class PathTests: XCTestCase {
    func testClockwiseArcBulgesUp() {
        let r = tight(SVGPath.parse("M0 0 a10 10 0 0 1 20 0"))
        XCTAssertEqual(r.minY, -10, accuracy: 0.01)
        XCTAssertEqual(r.maxX, 20, accuracy: 0.01)
    }

    func testQuadraticCurveIsDrawn() {
        let r = tight(SVGPath.parse("M21 29 q3 2.6 6 0")) // the calm face's smile
        XCTAssertEqual(r.width, 6, accuracy: 0.01)
        XCTAssertEqual(r.maxY, 30.3, accuracy: 0.01)
    }

    func testEveryFaceMarkHasAShape() {
        for (i, face) in CharacterLook.faces.enumerated() {
            for m in face { XCTAssertFalse(m.path.isEmpty, "face \(i)") }
        }
    }

    func testLargeArcTakesTheLongWay() {
        let small = tight(SVGPath.parse("M0 0 A10 10 0 0 1 10 10"))
        XCTAssertEqual(small.minY, 0, accuracy: 0.01)
        let large = tight(SVGPath.parse("M0 0 A10 10 0 1 1 10 10")) // the long way round, over the top
        XCTAssertEqual(large.minY, -10, accuracy: 0.01)
        XCTAssertEqual(large.maxX, 20, accuracy: 0.01)
    }

    func testEveryShapeStaysNearItsBox() {
        for s in CharacterLook.shapes {
            let r = tight(s.body)
            // the page draws the puff 1.5 past the box on the right (its svg overflows)
            XCTAssertTrue(CGRect(x: -2, y: -2, width: 52, height: 52).contains(r), "\(s.name) \(r)")
        }
    }

    func testToastIsAsDrawnByThePage() {
        let p = SVGPath.parse("M8 20 a10 10 0 0 1 10 -12 h12 a10 10 0 0 1 10 12 v16 a6 6 0 0 1 -6 6 h-20 a6 6 0 0 1 -6 -6 z")
        let r = tight(p)
        XCTAssertEqual(r.minX, 7.8, accuracy: 0.05); XCTAssertEqual(r.maxX, 40.2, accuracy: 0.05) // its corners bulge a little
        XCTAssertEqual(r.minY, 8, accuracy: 0.01); XCTAssertEqual(r.maxY, 42, accuracy: 0.01)
    }
}

final class StateTests: XCTestCase {
    func testWordsAndStatuses() {
        XCTAssertEqual(CharacterState.allCases.map(\.word), ["idle", "working", "needs you", "starting", "stopped"])
        XCTAssertEqual(CharacterState(status: "running"), .working)
        XCTAssertEqual(CharacterState(status: "waiting"), .needsYou)
        XCTAssertEqual(CharacterState(status: "exited"), .stopped)
        XCTAssertEqual(CharacterState(status: "finished"), .idle)
    }

    func testGroupShowsWorkingMembersFirstAndThreeAtMost() {
        let m: [GroupTile.Member] = ["a", "b", "c", "d", "e"].enumerated().map { (id: $1, kind: "", state: $0 == 4 ? .working : .idle) }
        XCTAssertEqual(GroupTile(members: m, size: 40).shown.map(\.id), ["e", "a", "b"])
        XCTAssertEqual(GroupTile(members: m, size: 40).word, "working")
        XCTAssertEqual(GroupTile(members: [], size: 40).word, "idle")
    }

    func testMotionIsAtRestAtTheStartOfAPeriod() {
        let h = CharacterMotion.hop(at: 0)
        XCTAssertEqual(h.dy, 0); XCTAssertEqual(h.sx, 1)
        XCTAssertLessThan(CharacterMotion.hop(at: CharacterMotion.hopPeriod * 0.45).dy, -0.07)
        XCTAssertEqual(CharacterMotion.blink(at: 0), 1)
        XCTAssertEqual(CharacterMotion.blink(at: CharacterMotion.blinkPeriod * 0.94), 0.1, accuracy: 0.001)
    }
}
