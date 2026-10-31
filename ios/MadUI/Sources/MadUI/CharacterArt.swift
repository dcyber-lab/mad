import SwiftUI

/// What a face is made of: strokes and dots, in coordinates with the middle between the eyes at 0, 0.
struct FaceMark {
    let path: Path
    /// Filled (a dot) rather than stroked.
    let filled: Bool
    let width: CGFloat
    let opacity: Double
}

extension Expression {
    /// The reference's `EXPR` strokes: eyes, brows and mouth, and the ? and z that float by the face.
    var marks: [FaceMark] { Self.table[self]! }

    private static func ink(_ d: String, opacity: Double = 1) -> FaceMark {
        FaceMark(path: SVGPath.parse(d), filled: false, width: 2.1, opacity: opacity)
    }
    private static func dot(_ x: CGFloat, _ y: CGFloat, _ r: CGFloat, opacity: Double = 1) -> FaceMark {
        FaceMark(path: Path(ellipseIn: CGRect(x: x - r, y: y - r, width: 2 * r, height: 2 * r)), filled: true, width: 0, opacity: opacity)
    }
    private static func ring(_ x: CGFloat, _ y: CGFloat, rx: CGFloat, ry: CGFloat) -> FaceMark {
        FaceMark(path: Path(ellipseIn: CGRect(x: x - rx, y: y - ry, width: 2 * rx, height: 2 * ry)), filled: false, width: 2.1, opacity: 1)
    }
    /// A bold letter drawn as strokes, its baseline's left end at x, y (the page sets it as text).
    private static func glyph(_ d: String, at x: CGFloat, _ y: CGFloat, width: CGFloat, opacity: Double = 1) -> FaceMark {
        FaceMark(path: SVGPath.parse(d).applying(CGAffineTransform(translationX: x, y: y)), filled: false, width: width, opacity: opacity)
    }
    private static let question = [
        glyph("M0.4 -5.2 a2.2 2.2 0 1 1 3.6 1.7 q-1.4 0.9 -1.4 2.4", at: 12, -9, width: 1.5),
        FaceMark(path: Path(ellipseIn: CGRect(x: 12 + 2.6 - 0.8, y: -9 - 0.8, width: 1.6, height: 1.6)), filled: true, width: 0, opacity: 1),
    ]

    private static let table: [Expression: [FaceMark]] = [
        .happy: [ink("M-7.5 1 q2.5 -3 5 0"), ink("M2.5 1 q2.5 -3 5 0"), ink("M-3.5 6 q3.5 3.5 7 0")],
        .focus: [dot(-5, 0, 1.9), dot(5, 0, 1.9), ink("M-7.5 -4.2 l5 0.3"), ink("M7.5 -4.2 l-5 0.3"), ink("M-2.5 7 h5")],
        .think: [dot(-4, -1.5, 1.9), dot(6, -1.5, 1.9), ink("M-1 7 q3 -1.5 6 0"), ink("M11 -9 q2 -3 0 -5", opacity: 0.7),
                 dot(12.5, -1, 1, opacity: 0.6)],
        .wow: [ring(-5, 0, rx: 2.6, ry: 2.6), ring(5, 0, rx: 2.6, ry: 2.6), ring(0, 7.5, rx: 2, ry: 2.6)],
        .wink: [dot(-5, 0, 1.9), ink("M2.5 0.5 h5"), ink("M-3.5 5.5 q3.5 3.5 7 0")],
        .ask: [dot(-5, 0, 1.9), dot(5, 0, 1.9), ink("M-8 -5 q3 -2 6 0"), ink("M2 -5.5 q3 -2.5 6 -1"),
               ink("M-2.5 7.5 q1.2 -1.2 2.5 0 q1.3 1.2 2.5 0")] + question,
        .calm: [dot(-5, 0, 1.9), dot(5, 0, 1.9), ink("M-3 6 q3 2.5 6 0")],
        .sleep: [ink("M-7.5 0 q2.5 2 5 0"), ink("M2.5 0 q2.5 2 5 0"), ink("M-2 7 h4"),
                 glyph("M0 -4.6 h3.6 l-3.6 4.6 h3.8", at: 11, -7, width: 1.2, opacity: 0.7)],
    ]
}

/// Draws one character into a 48-by-48 box of a Canvas: a pastel body with no outline, and the
/// face in the darkened strong color of its hue.
enum CharacterArt {
    /// box is where the 48-unit drawing goes.
    static func draw(_ ctx: inout GraphicsContext, look: CharacterLook, in box: CGRect, pose: CharacterPlan.Pose) {
        var c = ctx
        c.translateBy(x: box.minX, y: box.minY)
        c.scaleBy(x: box.width / 48, y: box.height / 48)
        c.translateBy(x: 0, y: pose.bob)

        let shape = CharacterLook.shapes[look.shape]
        // the head turns about the middle of its foot
        c.translateBy(x: shape.pivot.x, y: shape.pivot.y)
        c.rotate(by: .degrees(pose.tilt))
        c.translateBy(x: -shape.pivot.x, y: -shape.pivot.y)

        if !shape.leaves.isEmpty {
            var l = c
            l.translateBy(x: shape.leafPivot.x, y: shape.leafPivot.y)
            l.rotate(by: .degrees(pose.leaf))
            l.translateBy(x: -shape.leafPivot.x, y: -shape.leafPivot.y)
            for (leaf, color) in shape.leaves { l.fill(leaf, with: .color(color)) }
        }
        c.fill(shape.body, with: .color(Color(hex: UInt32(look.colorHex.dropFirst(), radix: 16)!)))

        c.translateBy(x: 24 + pose.look.x, y: 23 + shape.faceOffset + pose.look.y)
        let ink = look.faceColor
        for mark in pose.face.marks {
            let color = ink.opacity(mark.opacity)
            if mark.filled { c.fill(mark.path, with: .color(color)) }
            else { c.stroke(mark.path, with: .color(color), style: StrokeStyle(lineWidth: mark.width, lineCap: .round, lineJoin: .round)) }
        }
    }
}
