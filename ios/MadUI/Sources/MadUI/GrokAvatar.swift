import SwiftUI

/// An agent's picture: a solid, saturated circle or tilted rounded square with two slanted eyes, its color and
/// shape picked by the same stable hash of its id as the character's (so it is the same on the page's side).
/// It is drawn as the approved design's `av()` (v5) draws it, in a box of 100 units: the body takes the middle 80,
/// the eyes sit at 36 and 60. Working, the eyes look from side to side; stopped, its eyes are shut (the view fades
/// it). The ring around it that tells the state is `StatusRing`.
enum GrokAvatar {
    /// Eight saturated colors, indexed by `CharacterLook.color`.
    static let colors: [UInt32] = [0x8a3ffc, 0xff5a1f, 0x1f66ff, 0xff4d8d, 0x1fbf75, 0x12b5d6, 0xf5b324, 0x6a5cff]

    /// Draws the avatar in `rect` (a square of any size). `ms` is how the eyes are placed in time: nil is at rest.
    static func draw(_ ctx: inout GraphicsContext, look: CharacterLook, in rect: CGRect, state: CharacterState, at ms: Double? = nil) {
        let u = rect.width / 100
        var c = ctx
        c.translateBy(x: rect.minX, y: rect.minY)
        c.scaleBy(x: u, y: u)
        let body = Color(hex: colors[look.color % colors.count])
        if look.shape % 2 == 0 {
            c.fill(Path(ellipseIn: CGRect(x: 10, y: 10, width: 80, height: 80)), with: .color(body))
        } else {
            // <rect x=12 y=12 width=76 height=76 rx=26 transform="rotate(-12 50 50)">
            var t = c
            t.translateBy(x: 50, y: 50)
            t.rotate(by: .degrees(-12))
            t.fill(Path(roundedRect: CGRect(x: -38, y: -38, width: 76, height: 76), cornerRadius: 26, style: .continuous),
                   with: .color(body))
        }
        // the eyes: two short dark strokes, slanted the same way; they look about while it works, and are shut lines
        // when it is stopped
        let glance = state == .working ? sin((ms ?? 0) / 520) * 3.5 : 0
        for x in [36.0, 60.0] {
            if state == .stopped {
                c.fill(Path(roundedRect: CGRect(x: x - 7, y: 44, width: 14, height: 5), cornerRadius: 2.5),
                       with: .color(Color.black.opacity(0.75)))
            } else {
                var e = c
                e.translateBy(x: x + glance, y: 43)
                e.rotate(by: .degrees(-16))
                e.fill(Path(roundedRect: CGRect(x: -3.5, y: -9, width: 7, height: 18), cornerRadius: 3.5),
                       with: .color(Color.black.opacity(0.78)))
            }
        }
    }
}

/// The ring around an avatar that tells its state (the design's `av()`): waiting, an amber ring with the body drawn
/// at 82% inside it; working, a faint track with a green arc that turns (still under Reduce Motion); just finished,
/// a blue dot at the bottom right with a rim of what it sits on; idle and stopped, none.
struct StatusRing: View {
    let state: CharacterState
    let size: CGFloat
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.madAvatarBackdrop) private var backdrop
    @State private var spin = false

    /// The states drawn inside a ring, at 82%.
    static func ringed(_ state: CharacterState) -> Bool { state == .needsYou || state == .working }

    var body: some View {
        let u = size / 100
        ZStack {
            switch state {
            case .needsYou:
                Circle().inset(by: 2 * u).stroke(Color.madWait, lineWidth: 5 * u)
            case .working:
                Circle().inset(by: 2 * u).stroke(Color.madLine2, lineWidth: 4 * u)
                // stroke-dasharray 80 222 of a circle of r 48: 80 / 301.6 of the way round
                Circle().inset(by: 2 * u).trim(from: 0, to: 0.265)
                    .stroke(Color.madWork, style: StrokeStyle(lineWidth: 5 * u, lineCap: .round))
                    .rotationEffect(.degrees(spin ? 360 : 0))
                    .animation(reduceMotion ? nil : .linear(duration: 1.4).repeatForever(autoreverses: false), value: spin)
                    .onAppear { if !reduceMotion { spin = true } }
                    .onChange(of: reduceMotion) { _, still in spin = !still }
            case .done:
                // <circle cx=86 cy=86 r=13 fill=done stroke=paper stroke-width=5>
                Circle().fill(Color.madDone)
                    .overlay(Circle().stroke(backdrop, lineWidth: 5 * u))
                    .frame(width: 26 * u, height: 26 * u)
                    .position(x: 86 * u, y: 86 * u)
            default:
                EmptyView()
            }
        }
        .frame(width: size, height: size)
        .allowsHitTesting(false)
    }
}

private struct AvatarBackdropKey: EnvironmentKey { static let defaultValue = Color.madPaper }

extension EnvironmentValues {
    /// What avatars are drawn on (the sidebar, the paper): the rim of the done dot is of it, so it looks cut out.
    public var madAvatarBackdrop: Color {
        get { self[AvatarBackdropKey.self] }
        set { self[AvatarBackdropKey.self] = newValue }
    }
}
