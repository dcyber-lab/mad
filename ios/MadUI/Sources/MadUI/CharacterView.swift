import SwiftUI

/// An agent's character: one of eight shapes with a face and a color, picked by
/// a stable hash of its id, as on the web page. It hops and blinks while it
/// works (unless Reduce Motion is on), shows a red dot when it needs the person,
/// and is dimmed when stopped.
public struct CharacterView: View {
    let look: CharacterLook
    let state: CharacterState
    let size: CGFloat
    static let margin: CGFloat = 0.25
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    public init(id: String, kind: String, state: CharacterState, size: CGFloat) {
        self.init(look: CharacterLook(id: id, kind: kind), state: state, size: size)
    }

    init(look: CharacterLook, state: CharacterState, size: CGFloat) {
        self.look = look
        self.state = state
        self.size = size
    }

    public var body: some View {
        let moving = state == .working && !reduceMotion
        TimelineView(.animation(paused: !moving)) { tl in
            let t = tl.date.timeIntervalSinceReferenceDate
            Canvas { ctx, _ in
                var c = ctx
                // the canvas is larger than the box: like the page's svg, nothing clips a hop or a puff's edge
                CharacterArt.draw(&c, look: look, in: CGRect(x: size * Self.margin, y: size * Self.margin, width: size, height: size),
                                  hop: moving ? CharacterMotion.hop(at: t) : .init(),
                                  blink: moving ? CharacterMotion.blink(at: t) : 1)
            }
        }
        .frame(width: size * (1 + 2 * Self.margin), height: size * (1 + 2 * Self.margin))
        .frame(width: size, height: size)
        .opacity(state == .stopped ? 0.5 : 1)
        .accessibilityElement()
        .accessibilityLabel(state.word)
        .accessibilityAddTraits(.isImage)
        .cornerDot(state == .needsYou, size: size)
    }
}
