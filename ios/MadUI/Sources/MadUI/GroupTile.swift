import SwiftUI

/// A group chat's picture: a rounded tile with up to three members' characters,
/// small and huddled. Each wears the face of its own state, and the working and asking ones move as a lone character does.
public struct GroupTile: View {
    public typealias Member = (id: String, kind: String, state: CharacterState)

    let members: [Member]
    let size: CGFloat
    let dot: Bool?
    /// Its members' looks, built once: each is a hash, a random sequence and a shuffle.
    private let minis: [(look: CharacterLook, state: CharacterState)]
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var since = Date()
    @State private var onScreen = false

    /// dot: whether the red dot shows; by default, when a member needs the person.
    public init(members: [Member], size: CGFloat, dot: Bool? = nil) {
        self.dot = dot
        self.members = members
        minis = Self.shown(of: members).map { (look: CharacterLook(id: $0.id, kind: $0.kind), state: $0.state) }
        self.size = size
    }

    private static let positions: [CGRect] = [
        CGRect(x: 3, y: 6, width: 24, height: 24), CGRect(x: 21, y: 3, width: 22, height: 22), CGRect(x: 12, y: 22, width: 26, height: 26),
    ]

    private var needsYou: Bool { members.contains { $0.state == .needsYou } }

    /// What the tile says: working, else needs you, else starting, else idle.
    var word: String { CharacterState.group(members.map(\.state)).word }

    /// Working members first, then the rest in order; three at most.
    var shown: [Member] { Self.shown(of: members) }

    private static func shown(of members: [Member]) -> [Member] {
        let ranked = members.enumerated().sorted { a, b in
            let ra = a.element.state == .working ? 0 : 1, rb = b.element.state == .working ? 0 : 1
            return ra != rb ? ra < rb : a.offset < b.offset
        }
        return Array(ranked.prefix(3).map(\.element))
    }

    public var body: some View {
        Group {
            if reduceMotion || !animated {
                canvas(at: nil)
            } else {
                // the cycles are seconds long: 30 frames a second is plenty
                TimelineView(.animation(minimumInterval: 1 / 30, paused: !onScreen)) { canvas(at: $0.date) }
            }
        }
        .frame(width: size, height: size)
        .onAppear { onScreen = true }
        .onDisappear { onScreen = false }
        .accessibilityElement()
        .accessibilityLabel(word)
        .accessibilityAddTraits(.isImage)
        .cornerDot(dot ?? needsYou, size: size)
    }

    /// Something in the tile moves: a member works or asks.
    private var animated: Bool { shown.contains { $0.state == .working || $0.state == .needsYou } }

    /// The tile at `date`; nil is at rest: up to three avatars huddled on no plate, as a chat app draws a group.
    private func canvas(at date: Date?) -> some View {
        let ms = date.map { $0.timeIntervalSince(since) * 1000 }
        return Canvas { ctx, _ in
            var c = ctx
            c.scaleBy(x: size / 48, y: size / 48)
            if minis.isEmpty {
                c.fill(Path(ellipseIn: CGRect(x: 1, y: 1, width: 46, height: 46)), with: .color(.primary.opacity(0.1)))
                c.draw(Text("#").font(.system(size: 20, weight: .bold)).foregroundColor(.secondary), at: CGPoint(x: 24, y: 24))
            }
            // one alone fills the tile; two and three are huddled
            let spots: [[CGRect]] = [
                [CGRect(x: 0, y: 0, width: 48, height: 48)],
                [CGRect(x: 0, y: 2, width: 30, height: 30), CGRect(x: 18, y: 16, width: 30, height: 30)],
                [CGRect(x: 0, y: 1, width: 28, height: 28), CGRect(x: 20, y: 0, width: 28, height: 28), CGRect(x: 10, y: 20, width: 28, height: 28)],
            ]
            guard !minis.isEmpty else { return }
            for (i, mini) in minis.enumerated() {
                GrokAvatar.draw(&c, look: mini.look, in: spots[minis.count - 1][i], state: mini.state, at: mini.state == .working ? ms : nil)
            }
        }
    }
}
