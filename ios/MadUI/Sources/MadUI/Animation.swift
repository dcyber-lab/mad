import SwiftUI

/// The page's `hop` and `blink` keyframes (style.css), as functions of time.
enum CharacterMotion {
    static let hopPeriod = 1.15
    static let blinkPeriod = 2.8

    struct Hop { var dy: CGFloat = 0, sx: CGFloat = 1, sy: CGFloat = 1 }

    private static func ease(_ t: Double) -> CGFloat { CGFloat(t * t * (3 - 2 * t)) }
    private static func mix(_ a: CGFloat, _ b: CGFloat, _ t: CGFloat) -> CGFloat { a + (b - a) * t }

    /// hop at time t seconds, in units of a character's own height (the page's 3px of 40).
    static func hop(at t: Double, delay: Double = 0) -> Hop {
        let p = ((t - delay).truncatingRemainder(dividingBy: hopPeriod) + hopPeriod).truncatingRemainder(dividingBy: hopPeriod) / hopPeriod
        let up = Hop(dy: -3.0 / 40, sx: 0.97, sy: 1.04), down = Hop(dy: 0, sx: 1.05, sy: 0.95)
        func between(_ a: Hop, _ b: Hop, _ f: Double) -> Hop {
            let e = ease(f)
            return Hop(dy: mix(a.dy, b.dy, e), sx: mix(a.sx, b.sx, e), sy: mix(a.sy, b.sy, e))
        }
        if p < 0.45 { return between(Hop(), up, p / 0.45) }
        if p < 0.6 { return between(up, down, (p - 0.45) / 0.15) }
        return between(down, Hop(), (p - 0.6) / 0.4)
    }

    /// Eye height factor: open, shut to 0.1 at 94% of the period, open again.
    static func blink(at t: Double) -> CGFloat {
        let p = t.truncatingRemainder(dividingBy: blinkPeriod) / blinkPeriod
        if p < 0.9 || p >= 1 { return 1 }
        if p < 0.94 { return mix(1, 0.1, ease((p - 0.9) / 0.04)) }
        return mix(0.1, 1, ease((p - 0.94) / 0.06))
    }
}
