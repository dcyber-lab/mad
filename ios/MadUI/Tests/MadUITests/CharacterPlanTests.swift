import XCTest
@testable import MadUI

/// The plans and poses against the vectors dumped from the approved reference by
/// `node ios/MadUI/scripts/dump-character-vectors.js` (Fixtures/character-vectors.json).
final class CharacterPlanTests: XCTestCase {
    struct Vector: Decodable {
        struct Pt: Decodable { let x: Double, y: Double, t: Double
            init(from d: Decoder) throws { var c = try d.unkeyedContainer(); x = try c.decode(Double.self); y = try c.decode(Double.self); t = try c.decode(Double.self) } }
        struct Plan: Decodable { let style: String, dur: Double, tilt: Double, pts: [Pt], order: [String], beat: Double, delay: Double, quirk: Double }
        struct Work: Decodable { let ms: Double, x: Double, y: Double, tilt: Double, bob: Double, leaf: Double, expr: String }
        struct Need: Decodable { let ms: Double, tilt: Double }
        struct Idle: Decodable { let ms: Double, every: Double, blink: Bool }
        let id: String, hash: UInt32, plan: Plan, working: [Work], needs: [Need], idle: [Idle]
    }

    static let vectors: [Vector] = {
        let url = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("Fixtures/character-vectors.json")
        return try! JSONDecoder().decode([Vector].self, from: Data(contentsOf: url))
    }()

    let tol = 1e-6

    func testThereAreManyVectors() {
        XCTAssertGreaterThanOrEqual(Self.vectors.count, 27)
        XCTAssertEqual(Set(Self.vectors.map(\.plan.style)).count, 4, "all four styles are covered")
    }

    func testPlansMatchTheReferenceFieldByField() {
        for v in Self.vectors {
            let p = CharacterPlan(key: v.id)
            XCTAssertEqual(CharacterLook.hash(v.id), v.hash, v.id)
            XCTAssertEqual(p.style.rawValue, v.plan.style, v.id)
            XCTAssertEqual(p.dur, v.plan.dur, accuracy: tol, v.id)
            XCTAssertEqual(p.tilt, v.plan.tilt, accuracy: tol, v.id)
            XCTAssertEqual(p.beat, v.plan.beat, accuracy: tol, v.id)
            XCTAssertEqual(p.delay, v.plan.delay, accuracy: tol, v.id)
            XCTAssertEqual(p.quirk, v.plan.quirk, accuracy: tol, v.id)
            XCTAssertEqual(p.order.map(\.rawValue), v.plan.order, "the order of \(v.id)")
            XCTAssertEqual(p.pts.count, v.plan.pts.count, "points of \(v.id)")
            for (a, b) in zip(p.pts, v.plan.pts) {
                XCTAssertEqual(a.x, b.x, accuracy: tol, v.id); XCTAssertEqual(a.y, b.y, accuracy: tol, v.id); XCTAssertEqual(a.t, b.t, accuracy: tol, v.id)
            }
        }
    }

    func testWorkingPosesMatch() {
        for v in Self.vectors {
            let p = CharacterPlan(key: v.id)
            for w in v.working {
                let pose = p.pose(.working, ms: w.ms)
                let at = "\(v.id) at \(w.ms) ms"
                XCTAssertEqual(pose.look.x, w.x, accuracy: tol, at); XCTAssertEqual(pose.look.y, w.y, accuracy: tol, at)
                XCTAssertEqual(pose.tilt, w.tilt, accuracy: tol, at)
                XCTAssertEqual(pose.bob, w.bob, accuracy: tol, at)
                XCTAssertEqual(pose.leaf, w.leaf, accuracy: tol, at)
                XCTAssertEqual(pose.face.rawValue, w.expr, at)
            }
        }
    }

    func testAskingAndIdlePosesMatch() {
        for v in Self.vectors {
            let p = CharacterPlan(key: v.id)
            for n in v.needs {
                let pose = p.pose(.needsYou, ms: n.ms)
                XCTAssertEqual(pose.tilt, n.tilt, accuracy: tol, v.id)
                XCTAssertEqual(pose.face, .ask)
            }
            for i in v.idle {
                XCTAssertEqual(p.blinkEvery, i.every, v.id)
                XCTAssertEqual(p.pose(.idle, ms: i.ms).face, i.blink ? .sleep : .calm, "\(v.id) at \(i.ms) ms")
            }
        }
    }

    func testStoppedSleepsAndStartingIsIdle() {
        let p = CharacterPlan(key: "architect")
        XCTAssertEqual(p.pose(.stopped, ms: 5000).face, .sleep)
        XCTAssertEqual(p.pose(.starting, ms: 0).face, .calm)
    }

    func testStillMeansUprightAndAWorkingOneStaysFocused() {
        XCTAssertEqual(CharacterPlan.resting(.working), CharacterPlan.Pose(face: .focus), "not calm: that would look idle")
        XCTAssertEqual(CharacterPlan.resting(.needsYou).face, .ask)
        XCTAssertEqual(CharacterPlan.resting(.needsYou).tilt, 0)
        XCTAssertEqual(CharacterPlan.resting(.stopped).face, .sleep)
        XCTAssertEqual(CharacterPlan.resting(.idle).face, .calm)
    }

    func testAWorkingOneWaitsOutItsDelayBeforeLookingAround() {
        let p = CharacterPlan(key: "architect")
        let early = p.working(ms: p.delay / 2)
        XCTAssertEqual(early.look.x, 0); XCTAssertEqual(early.look.y, 0); XCTAssertEqual(early.tilt, 0)
        XCTAssertNotEqual(early.bob, 0) // it bobs from the first moment
    }

    func testTheFaceColorIsTheStrongColorDarkened55Percent() {
        XCTAssertEqual(CharacterLook.mix("#6f9bff", 0.55), 0x324673)
        XCTAssertEqual(CharacterLook.mix("#ffffff", 0.55), 0x737373)
    }

    func testEaseIsCSSEaseInOut() {
        XCTAssertEqual(CharacterPlan.ease(0), 0, accuracy: 1e-9)
        XCTAssertEqual(CharacterPlan.ease(1), 1, accuracy: 1e-9)
        XCTAssertEqual(CharacterPlan.ease(0.5), 0.5, accuracy: 1e-6)
        XCTAssertEqual(CharacterPlan.ease(0.25), 0.129, accuracy: 0.002)
    }

    func testBlinkScheduleLandsOnTheEdges() {
        let origin = Date(timeIntervalSinceReferenceDate: 1000)
        var it = BlinkSchedule.Entries(origin: origin, every: 3, last: origin.addingTimeInterval(0.5), first: true)
        let got = (0..<5).compactMap { _ in it.next() }.map { $0.timeIntervalSince(origin) }
        XCTAssertEqual(got.count, 5)
        for (a, b) in zip(got, [0.5, 3, 3.14, 6, 6.14]) { XCTAssertEqual(a, b, accuracy: 1e-6) }
    }

    // MARK: the web page's own vectors

    struct WebVector: Decodable {
        let id: String, shape: Int, colour: Int, style: String, dur: Double, tilt: Double, order: [String]
        let beat: Double, delay: Double, quirk: Double, pts: [Vector.Pt]
    }

    /// internal/desktop/web/testdata/character-vectors.json, written by the web implementation
    /// (copied to Fixtures/web-character-vectors.json; regenerate with
    /// `node internal/desktop/testdata/dump_character_vectors.js`).
    func testPlansMatchTheWebPagesVectors() throws {
        let url = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("Fixtures/web-character-vectors.json")
        let web = try JSONDecoder().decode([WebVector].self, from: Data(contentsOf: url))
        XCTAssertGreaterThanOrEqual(web.count, 20)
        for v in web {
            let look = CharacterLook(id: v.id), p = look.plan
            XCTAssertEqual(look.shape, v.shape, v.id)
            XCTAssertEqual(look.color, v.colour, v.id)
            XCTAssertEqual(p.style.rawValue, v.style, v.id)
            XCTAssertEqual(p.dur, v.dur, accuracy: 1e-9, v.id)
            XCTAssertEqual(p.tilt, v.tilt, accuracy: 1e-9, v.id)
            XCTAssertEqual(p.beat, v.beat, accuracy: 1e-9, v.id)
            XCTAssertEqual(p.delay, v.delay, accuracy: 1e-9, v.id)
            XCTAssertEqual(p.quirk, v.quirk, accuracy: 1e-9, v.id)
            XCTAssertEqual(p.order.map(\.rawValue), v.order, v.id)
            XCTAssertEqual(p.pts.count, v.pts.count, v.id)
            for (a, b) in zip(p.pts, v.pts) {
                XCTAssertEqual(a.x, b.x, accuracy: 1e-9, v.id); XCTAssertEqual(a.y, b.y, accuracy: 1e-9, v.id); XCTAssertEqual(a.t, b.t, accuracy: 1e-9, v.id)
            }
        }
    }
}
