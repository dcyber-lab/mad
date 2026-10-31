import Foundation

/// The eight faces a character wears, as the reference's `EXPR`.
enum Expression: String, CaseIterable {
    case happy, focus, think, wow, wink, ask, calm, sleep
}

/// How one character moves while it works: where it looks, how it turns, which faces it wears in
/// which order and how fast, all drawn from its id so no two bots move alike. This is the
/// reference's `plan(id)` exactly (an xorshift seeded by the FNV hash of the id, and a shuffle of
/// the faces that is V8's own sort run with the same random comparator), and the samplers below
/// are what the Web Animations API does with the keyframes it builds from it.
struct CharacterPlan: Equatable {
    enum Style: String { case wander, circle, scan, peek }

    struct Point: Equatable { var x: Double, y: Double, t: Double }

    var style: Style
    /// One look-around, in milliseconds.
    var dur: Double
    /// Degrees of head turn per 4 units of face travel to the side.
    var tilt: Double
    var pts: [Point]
    var order: [Expression]
    /// How long a face stays, in milliseconds.
    var beat: Double
    /// Before it starts looking around, in milliseconds.
    var delay: Double
    /// One breath of the bob, in milliseconds.
    var quirk: Double
    /// How often an idle character blinks, in milliseconds.
    var blinkEvery: Double

    init(key: String) {
        var r = Rng(seed: CharacterLook.hash(key))
        let styles: [Style] = [.wander, .circle, .scan, .peek]
        style = styles[Int((r.next() * 4).rounded(.down))]
        dur = 2800 + r.next() * 3600
        let reach = 2.2 + r.next() * 1.6
        tilt = 3 + r.next() * 6
        var pts: [Point] = []
        switch style {
        case .circle:
            let dir: Double = r.next() < 0.5 ? 1 : -1
            let st = r.next() * .pi * 2
            for i in 0...10 {
                let a = st + dir * Double(i) / 10 * .pi * 2
                pts.append(Point(x: cos(a) * reach, y: sin(a) * reach * 0.8, t: Double(i) / 10))
            }
        case .scan:
            let c: [(Double, Double)] = [(-1, -1), (1, -1), (1, 1), (-1, 1)]
            let o = Int((r.next() * 4).rounded(.down))
            pts.append(Point(x: 0, y: 0, t: 0))
            for i in 0..<4 {
                let (x, y) = c[(i + o) % 4]
                let t = 0.1 + Double(i) * 0.2
                pts.append(Point(x: x * reach * 0.85, y: y * reach * 0.7, t: t))
                pts.append(Point(x: x * reach * 0.85, y: y * reach * 0.7, t: t + 0.12))
            }
            pts.append(Point(x: 0, y: 0, t: 1))
        case .peek:
            let a = r.next() * .pi * 2
            let b = a + .pi * (0.6 + r.next() * 0.8)
            let ax = cos(a) * reach, ay = sin(a) * reach * 0.8
            pts = [Point(x: 0, y: 0, t: 0), Point(x: 0, y: 0, t: 0.32), Point(x: ax, y: ay, t: 0.38), Point(x: ax, y: ay, t: 0.5),
                   Point(x: 0, y: 0, t: 0.58), Point(x: 0, y: 0, t: 0.78),
                   Point(x: cos(b) * reach * 0.8, y: sin(b) * reach * 0.7, t: 0.83), Point(x: 0, y: 0, t: 0.92), Point(x: 0, y: 0, t: 1)]
        case .wander:
            pts.append(Point(x: 0, y: 0, t: 0))
            var t = 0.0
            let n = 4 + Int((r.next() * 3).rounded(.down))
            for _ in 0..<n {
                let a = r.next() * .pi * 2
                let d = reach * (0.5 + r.next() * 0.5)
                t += 0.7 / Double(n)
                let x = cos(a) * d, y = sin(a) * d * 0.8
                pts.append(Point(x: x, y: y, t: t))
                pts.append(Point(x: x, y: y, t: min(0.95, t + 0.25 / Double(n))))
                t += 0.25 / Double(n)
            }
            pts.append(Point(x: 0, y: 0, t: 1))
        }
        self.pts = pts
        var faces: [Expression] = [.focus, .think, .happy, .wow, .wink]
        V8Sort.sort(&faces) { r.next() - 0.5 }
        order = faces
        beat = 1500 + r.next() * 2200
        delay = r.next() * 1200
        quirk = 900 + r.next() * 1200
        blinkEvery = 2500 + Double(CharacterLook.hash(key) % 3000)
    }

    // MARK: poses

    /// What a character shows: the face's travel (units of the 48-unit drawing), the head's turn in
    /// degrees, the body's lift (negative is up), the sprout's leaf turn in degrees, and the face.
    struct Pose: Equatable {
        var face: Expression = .calm
        var look = (x: 0.0, y: 0.0)
        var tilt = 0.0
        var bob = 0.0
        var leaf = 0.0

        static func == (a: Pose, b: Pose) -> Bool {
            a.face == b.face && a.look == b.look && a.tilt == b.tilt && a.bob == b.bob && a.leaf == b.leaf
        }
    }

    /// The pose `ms` milliseconds after the character began its state. A working one looks around,
    /// bobs and changes faces; one that needs the person asks, swaying; an idle one is calm and
    /// blinks (a shut-eyed face for 140 ms every `blinkEvery`); a stopped one sleeps.
    func pose(_ state: CharacterState, ms: Double) -> Pose {
        switch state {
        case .working:
            return working(ms: ms)
        case .needsYou:
            return Pose(face: .ask, tilt: -4 + 8 * Self.alternate(ms, 900))
        case .stopped:
            return Pose(face: .sleep)
        case .idle, .starting:
            let blinking = ms >= blinkEvery && ms.truncatingRemainder(dividingBy: blinkEvery) < 140
            return Pose(face: blinking ? .sleep : .calm)
        }
    }

    func working(ms: Double) -> Pose {
        var p = Pose()
        let local = ms - delay
        if local >= 0 {
            let f = Self.ease(local.truncatingRemainder(dividingBy: dur) / dur)
            p.look = along(f)
            p.tilt = p.look.x / 4 * tilt
        }
        p.bob = -1.3 * Self.alternate(ms, quirk)
        p.leaf = -12 + 24 * Self.alternate(ms, quirk * 0.8)
        p.face = order[Int((ms / beat).rounded(.down)) % order.count]
        return p
    }

    /// The keyframes' value at progress `f`: straight lines between the points by offset.
    func along(_ f: Double) -> (x: Double, y: Double) {
        guard let first = pts.first, let last = pts.last else { return (0, 0) }
        if f <= first.t { return (first.x, first.y) }
        for i in 1..<pts.count where f <= pts[i].t {
            let a = pts[i - 1], b = pts[i], span = b.t - a.t
            if span <= 0 { return (b.x, b.y) }
            let u = (f - a.t) / span
            return (a.x + (b.x - a.x) * u, a.y + (b.y - a.y) * u)
        }
        return (last.x, last.y)
    }

    /// An alternating animation of `dur` ms: forward, then back, each eased.
    static func alternate(_ ms: Double, _ dur: Double) -> Double {
        let p = (ms / dur).truncatingRemainder(dividingBy: 2)
        return ease(p <= 1 ? p : 2 - p)
    }

    /// `cubic-bezier(.42, 0, .58, 1)`, CSS's ease-in-out: the value at time `x` in 0...1.
    static func ease(_ x: Double) -> Double {
        func bez(_ a: Double, _ b: Double, _ t: Double) -> Double {
            3 * a * t * (1 - t) * (1 - t) + 3 * b * t * t * (1 - t) + t * t * t
        }
        var lo = 0.0, hi = 1.0
        for _ in 0..<40 {
            let m = (lo + hi) / 2
            if bez(0.42, 0.58, m) < x { lo = m } else { hi = m }
        }
        return bez(0, 1, (lo + hi) / 2)
    }
}

/// The reference's `rng`: xorshift32, as JavaScript computes it (its shifts work on signed 32-bit
/// values, so `>> 17` copies the sign), returning a fraction of 100000 in 0..<1.
struct Rng {
    private var s: UInt32

    init(seed: UInt32) { s = seed == 0 ? 1 : seed }

    mutating func next() -> Double {
        var a = Int32(bitPattern: s)
        a ^= a &<< 13
        var b = a
        b ^= b >> 17
        b ^= b &<< 5
        s = UInt32(bitPattern: b)
        return Double(s % 100000) / 100000
    }
}

/// V8's `Array.prototype.sort` (TimSort) for an array too short to merge: one run found, the rest
/// put in by binary insertion. The reference shuffles with `sort(() => r() - .5)`, so which order
/// comes out depends on how many times, and where, the sort asks the comparator.
enum V8Sort {
    /// `compare()` answers what `comparefn(a, b)` would, which the reference ignores the arguments of.
    static func sort<T>(_ a: inout [T], compare: () -> Double) {
        let n = a.count
        guard n >= 2, n < 64 else { return } // 64 and more split into runs and merge: not needed here
        var run = countRun(&a, 0, n, compare)
        if run < n { binaryInsertion(&a, 0, run, n, compare); run = n }
    }

    private static func countRun<T>(_ a: inout [T], _ lowArg: Int, _ high: Int, _ compare: () -> Double) -> Int {
        let low = lowArg + 1
        if low == high { return 1 }
        var runLength = 2
        let descending = compare() < 0
        var idx = low + 1
        while idx < high {
            let order = compare()
            if descending { if order >= 0 { break } } else if order < 0 { break }
            runLength += 1
            idx += 1
        }
        if descending { a[lowArg..<(lowArg + runLength)].reverse() }
        return runLength
    }

    private static func binaryInsertion<T>(_ a: inout [T], _ low: Int, _ startArg: Int, _ high: Int, _ compare: () -> Double) {
        var start = low == startArg ? startArg + 1 : startArg
        while start < high {
            var left = low, right = start
            let pivot = a[start]
            while left < right {
                let mid = left + ((right - left) >> 1)
                if compare() < 0 { right = mid } else { left = mid + 1 }
            }
            var p = start
            while p > left { a[p] = a[p - 1]; p -= 1 }
            a[left] = pivot
            start += 1
        }
    }
}
