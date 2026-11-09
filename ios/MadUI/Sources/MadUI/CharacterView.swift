import SwiftUI

/// An agent's character: one of eight shapes in one of eight colors, picked by a stable hash of its id, with the
/// ring of the approved design (v5) around it for its state: waiting, an amber ring; working, a turning green arc;
/// just finished, a blue dot; stopped, the whole of it at 45% with its eyes shut; idle, nothing. A working one's
/// eyes look about. With Reduce Motion on, or when off screen, it keeps still (the arc too).
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

    /// dot: whether something waits on the person here; it draws the waiting ring even when the agent's own state
    /// does not say so (a row says it itself).
    public init(id: String, kind: String, state: CharacterState, size: CGFloat, dot: Bool? = nil) {
        self.init(look: CharacterLook(id: id, kind: kind), state: state, size: size, dot: dot)
    }

    init(look: CharacterLook, state: CharacterState, size: CGFloat, dot: Bool? = nil) {
        self.dot = dot
        self.look = look
        self.state = state
        self.size = size
    }

    /// The state the ring shows: waiting when something waits on the person (and the agent is not stopped).
    var ringState: CharacterState {
        if dot == true && state != .stopped { return .needsYou }
        return state
    }

    public var body: some View {
        let ring = ringState
        ZStack {
            Group {
                if reduceMotion || state != .working {
                    canvas(at: nil)
                } else {
                    // only a working one's eyes move, at 15 frames a second
                    TimelineView(.animation(minimumInterval: 1 / 15, paused: !onScreen)) { canvas(at: $0.date) }
                }
            }
            .scaleEffect(StatusRing.ringed(ring) ? 0.82 : 1)
            StatusRing(state: ring, size: size)
        }
        .frame(width: size, height: size)
        .opacity(state == .stopped ? 0.45 : 1)
        .onAppear { onScreen = true }
        .onDisappear { onScreen = false }
        .onChange(of: state) { since = Date() }
        .accessibilityElement()
        .accessibilityLabel(ring.word)
        .accessibilityAddTraits(.isImage)
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
