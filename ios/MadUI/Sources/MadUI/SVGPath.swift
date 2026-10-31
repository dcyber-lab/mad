import SwiftUI

/// Parses SVG path data (M L H V C A Z, absolute and relative) into a Path, so
/// the characters are drawn from the very strings the web page uses.
enum SVGPath {
    static func parse(_ d: String) -> Path {
        var tokens = Tokens(d)
        var path = Path()
        var cur = CGPoint.zero
        var start = CGPoint.zero
        var cmd: Character = "M"
        while let c = tokens.command(after: cmd) {
            cmd = c
            let rel = c.isLowercase
            func pt(_ x: CGFloat, _ y: CGFloat) -> CGPoint { rel ? CGPoint(x: cur.x + x, y: cur.y + y) : CGPoint(x: x, y: y) }
            switch Character(c.uppercased()) {
            case "M":
                guard let x = tokens.number(), let y = tokens.number() else { return path }
                cur = pt(x, y); start = cur; path.move(to: cur)
                cmd = rel ? "l" : "L" // further pairs are lineto
            case "L":
                guard let x = tokens.number(), let y = tokens.number() else { return path }
                cur = pt(x, y); path.addLine(to: cur)
            case "H":
                guard let x = tokens.number() else { return path }
                cur = CGPoint(x: rel ? cur.x + x : x, y: cur.y); path.addLine(to: cur)
            case "V":
                guard let y = tokens.number() else { return path }
                cur = CGPoint(x: cur.x, y: rel ? cur.y + y : y); path.addLine(to: cur)
            case "C":
                guard let a = tokens.number(), let b = tokens.number(), let c1 = tokens.number(),
                      let d1 = tokens.number(), let e = tokens.number(), let f = tokens.number() else { return path }
                let p1 = pt(a, b), p2 = pt(c1, d1), p3 = pt(e, f)
                path.addCurve(to: p3, control1: p1, control2: p2); cur = p3
            case "A":
                guard let rx = tokens.number(), let ry = tokens.number(), let rot = tokens.number(),
                      let large = tokens.number(), let sweep = tokens.number(),
                      let x = tokens.number(), let y = tokens.number() else { return path }
                let end = pt(x, y)
                arc(&path, from: cur, to: end, rx: rx, ry: ry, rotation: rot, large: large != 0, sweep: sweep != 0)
                cur = end
            case "Z":
                path.closeSubpath(); cur = start
            default:
                return path
            }
        }
        return path
    }

    /// SVG's endpoint arc, as cubic Béziers (SVG 1.1 appendix F.6).
    private static func arc(_ path: inout Path, from p0: CGPoint, to p1: CGPoint, rx rx0: CGFloat, ry ry0: CGFloat,
                            rotation: CGFloat, large: Bool, sweep: Bool) {
        if p0 == p1 { return }
        var rx = abs(rx0), ry = abs(ry0)
        if rx == 0 || ry == 0 { path.addLine(to: p1); return }
        let phi = rotation * .pi / 180
        let cp = cos(phi), sp = sin(phi)
        let dx = (p0.x - p1.x) / 2, dy = (p0.y - p1.y) / 2
        let x1 = cp * dx + sp * dy, y1 = -sp * dx + cp * dy
        let lam = (x1 * x1) / (rx * rx) + (y1 * y1) / (ry * ry)
        if lam > 1 { rx *= lam.squareRoot(); ry *= lam.squareRoot() }
        let num = rx * rx * ry * ry - rx * rx * y1 * y1 - ry * ry * x1 * x1
        let den = rx * rx * y1 * y1 + ry * ry * x1 * x1
        var k = den == 0 ? 0 : (max(0, num / den)).squareRoot()
        if large == sweep { k = -k }
        let cxp = k * rx * y1 / ry, cyp = -k * ry * x1 / rx
        let cx = cp * cxp - sp * cyp + (p0.x + p1.x) / 2
        let cy = sp * cxp + cp * cyp + (p0.y + p1.y) / 2
        func angle(_ ux: CGFloat, _ uy: CGFloat, _ vx: CGFloat, _ vy: CGFloat) -> CGFloat {
            atan2(ux * vy - uy * vx, ux * vx + uy * vy)
        }
        let th1 = angle(1, 0, (x1 - cxp) / rx, (y1 - cyp) / ry)
        var dth = angle((x1 - cxp) / rx, (y1 - cyp) / ry, (-x1 - cxp) / rx, (-y1 - cyp) / ry)
        if !sweep && dth > 0 { dth -= 2 * .pi }
        if sweep && dth < 0 { dth += 2 * .pi }
        let n = max(1, Int((abs(dth) / (.pi / 2)).rounded(.up)))
        let step = dth / CGFloat(n)
        let t = 4.0 / 3.0 * tan(step / 4)
        var a = th1
        for _ in 0..<n {
            let b = a + step
            func ell(_ ang: CGFloat) -> CGPoint {
                CGPoint(x: cx + rx * cos(ang) * cp - ry * sin(ang) * sp, y: cy + rx * cos(ang) * sp + ry * sin(ang) * cp)
            }
            func der(_ ang: CGFloat) -> CGPoint {
                CGPoint(x: -rx * sin(ang) * cp - ry * cos(ang) * sp, y: -rx * sin(ang) * sp + ry * cos(ang) * cp)
            }
            let s = ell(a), e = ell(b), ds = der(a), de = der(b)
            path.addCurve(to: e, control1: CGPoint(x: s.x + t * ds.x, y: s.y + t * ds.y),
                          control2: CGPoint(x: e.x - t * de.x, y: e.y - t * de.y))
            a = b
        }
    }

    private struct Tokens {
        let s: [Character]
        var i = 0
        init(_ d: String) { s = Array(d) }

        mutating func skip() { while i < s.count, s[i] == " " || s[i] == "," || s[i] == "\n" { i += 1 } }

        /// The next command letter; a number where a letter would be repeats the last command.
        mutating func command(after last: Character) -> Character? {
            skip()
            guard i < s.count else { return nil }
            if s[i].isLetter { i += 1; return s[i - 1] }
            return last
        }

        mutating func number() -> CGFloat? {
            skip()
            let from = i
            if i < s.count, s[i] == "-" || s[i] == "+" { i += 1 }
            var dot = false
            while i < s.count, s[i].isNumber || (s[i] == "." && !dot) {
                if s[i] == "." { dot = true }
                i += 1
            }
            return Double(String(s[from..<i])).map { CGFloat($0) }
        }
    }
}
