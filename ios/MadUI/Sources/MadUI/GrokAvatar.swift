import SwiftUI

/// An agent's picture: a solid, saturated circle or tilted rounded square with two slanted eyes, its color and
/// shape picked by the same stable hash of its id as the character's (so it is the same on the page's side).
/// Working, the eyes look from side to side; stopped, it is dimmed with its eyes shut.
enum GrokAvatar {
    /// Eight saturated colors, indexed by `CharacterLook.color`.
    static let colors: [UInt32] = [0x8a3ffc, 0xff5a1f, 0x1f66ff, 0xff4d8d, 0x1fbf75, 0x12b5d6, 0xf5b324, 0x6a5cff]

    /// Draws the avatar in `rect` (any size; the shape is drawn in its own box). `look` is how the eyes are placed in
    /// time: nil is at rest.
    static func draw(_ ctx: inout GraphicsContext, look: CharacterLook, in rect: CGRect, state: CharacterState, at ms: Double? = nil) {
        let s = rect.width
        var c = ctx
        if state == .stopped { c.opacity = 0.5 }
        let body = Color(hex: colors[look.color % colors.count])
        if look.shape % 2 == 0 {
            c.fill(Path(ellipseIn: rect), with: .color(body))
        } else {
            var t = c
            t.translateBy(x: rect.midX, y: rect.midY)
            t.rotate(by: .degrees(-12))
            t.fill(Path(roundedRect: CGRect(x: -s / 2, y: -s / 2, width: s, height: s), cornerRadius: s * 0.34, style: .continuous),
                   with: .color(body))
        }
        // the eyes: two short dark strokes, slanted the same way; they look about while it works
        let look = state == .working ? sin((ms ?? 0) / 520) * s * 0.035 : 0
        let eye = Color.black.opacity(0.78)
        for x in [0.35, 0.62] {
            var e = c
            e.translateBy(x: rect.minX + s * x + look, y: rect.minY + s * 0.42)
            if state != .stopped { e.rotate(by: .degrees(-16)) }
            let w = state == .stopped ? s * 0.16 : s * 0.075, h = state == .stopped ? s * 0.05 : s * 0.2
            e.fill(Path(roundedRect: CGRect(x: -w / 2, y: -h / 2, width: w, height: h), cornerRadius: min(w, h) / 2), with: .color(eye))
        }
    }
}
