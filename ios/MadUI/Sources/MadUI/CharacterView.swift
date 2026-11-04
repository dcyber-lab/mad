import SwiftUI

/// An agent's character: one of eight shapes in one of eight pastels, picked by a stable hash of
/// its id, with a face that changes with what it does. Working, it looks around, bobs and changes
/// faces in its own way and time; needing the person, it asks, swaying (and shows a red dot); idle,
/// it is calm and blinks now and then; stopped, it sleeps, dimmed. With Reduce Motion on, or when
/// off screen, it keeps still.
public struct CharacterView: View {
    let look: CharacterLook
    let state: CharacterState
    let size: CGFloat
    let dot: Bool?
    static let margin: CGFloat = 0.25
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// When the current state began: a character's motion starts from rest then.
    @State private var since = Date()
    @State private var onScreen = false

    /// dot: whether the red dot shows; by default, when the state is needs-you.
    /// A row says it itself when something waits on the person without the agent's state saying so.
    public init(id: String, kind: String, state: CharacterState, size: CGFloat, dot: Bool? = nil) {
        self.init(look: CharacterLook(id: id, kind: kind), state: state, size: size, dot: dot)
    }

    init(look: CharacterLook, state: CharacterState, size: CGFloat, dot: Bool? = nil) {
        self.dot = dot
        self.look = look
        self.state = state
        self.size = size
    }

    public var body: some View {
        Group {
            if reduceMotion || state != .working {
                canvas(at: nil)
            } else {
                // only a working one moves: its eyes, at 15 frames a second
                TimelineView(.animation(minimumInterval: 1 / 15, paused: !onScreen)) { canvas(at: $0.date) }
            }
        }
        .frame(width: size, height: size)
        .onAppear { onScreen = true }
        .onDisappear { onScreen = false }
        .onChange(of: state) { since = Date() }
        .accessibilityElement()
        .accessibilityLabel(state.word)
        .accessibilityAddTraits(.isImage)
        .cornerDot(dot ?? (state == .needsYou), size: size)
    }

    /// The avatar at `date`; nil is at rest.
    private func canvas(at date: Date?) -> some View {
        let ms = date.map { $0.timeIntervalSince(since) * 1000 }
        return Canvas { ctx, _ in
            var c = ctx
            GrokAvatar.draw(&c, look: look, in: CGRect(x: 0, y: 0, width: size, height: size), state: state, at: ms)
        }
    }

}

/// When an idle character's face changes: at each blink's start and end (and now).
struct BlinkSchedule: TimelineSchedule {
    let origin: Date
    /// Seconds between blinks.
    let every: Double

    func entries(from start: Date, mode: TimelineScheduleMode) -> Entries {
        Entries(origin: origin, every: every, last: start, first: true)
    }

    struct Entries: Sequence, IteratorProtocol {
        let origin: Date
        let every: Double
        var last: Date
        var first: Bool

        mutating func next() -> Date? {
            if first { first = false; return last }
            let e = last.timeIntervalSince(origin)
            let k = (e / every).rounded(.down)
            let edges = [k * every, k * every + 0.14, (k + 1) * every, (k + 1) * every + 0.14]
            let next = edges.first { $0 > e + 0.000_001 }!
            last = origin.addingTimeInterval(next)
            return last
        }
    }
}
