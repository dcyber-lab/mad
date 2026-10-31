import SwiftUI

/// Draws one character into a 48-by-48 box of a Canvas, as the page's `charSVG`.
enum CharacterArt {
    static let ink = Color(hex: 0x1d1d1f)

    /// box is where the 48-unit drawing goes; hop moves and squashes it about its
    /// bottom middle (in units of the box); blink is the eyes' height factor.
    static func draw(_ ctx: inout GraphicsContext, look: CharacterLook, in box: CGRect,
                     hop: CharacterMotion.Hop = .init(), blink: CGFloat = 1) {
        var c = ctx
        c.translateBy(x: box.minX, y: box.minY)
        c.scaleBy(x: box.width / 48, y: box.height / 48)
        c.translateBy(x: 24, y: 48 + hop.dy * 48)
        c.scaleBy(x: hop.sx, y: hop.sy)
        c.translateBy(x: -24, y: -48)

        let shape = CharacterLook.shapes[look.shape]
        for (leaf, color) in shape.leaves { c.fill(leaf, with: .color(color)) }
        c.fill(shape.body, with: .color(Color(hex: UInt32(look.colorHex.dropFirst(), radix: 16)!)))

        c.translateBy(x: 0, y: shape.faceOffset)
        for mark in CharacterLook.faces[look.face] {
            switch mark.kind {
            case .eyeDot, .eyeArc:
                let r = mark.path.cgPath.boundingBoxOfPath
                var e = c
                e.translateBy(x: r.midX, y: r.midY)
                e.scaleBy(x: 1, y: blink)
                e.translateBy(x: -r.midX, y: -r.midY)
                if mark.kind == .eyeDot { e.fill(mark.path, with: .color(ink)) }
                else { e.stroke(mark.path, with: .color(ink), style: StrokeStyle(lineWidth: 2.2, lineCap: .round)) }
            case .mouth: c.stroke(mark.path, with: .color(ink), style: StrokeStyle(lineWidth: 2, lineCap: .round))
            case .mouthDot: c.fill(mark.path, with: .color(ink))
            }
        }
    }
}
