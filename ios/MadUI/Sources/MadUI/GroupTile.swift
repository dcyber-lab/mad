import SwiftUI

/// A group chat's picture: a rounded tile with up to three members' characters,
/// small and huddled. They hop in turn while any member works, as the page's `groupChar`.
public struct GroupTile: View {
    public typealias Member = (id: String, kind: String, state: CharacterState)

    let members: [Member]
    let size: CGFloat
    let dot: Bool?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// dot: whether the red dot shows; by default, when a member needs the person.
    public init(members: [Member], size: CGFloat, dot: Bool? = nil) {
        self.dot = dot
        self.members = members
        self.size = size
    }

    private static let positions: [CGRect] = [
        CGRect(x: 3, y: 6, width: 24, height: 24), CGRect(x: 21, y: 3, width: 22, height: 22), CGRect(x: 12, y: 22, width: 26, height: 26),
    ]

    private var working: Bool { members.contains { $0.state == .working } }
    private var needsYou: Bool { members.contains { $0.state == .needsYou } }

    /// What the tile says: working, else needs you, else starting, else idle.
    var word: String {
        working ? "working" : needsYou ? "needs you" : members.contains { $0.state == .starting } ? "starting" : "idle"
    }

    /// Working members first, then the rest in order; three at most.
    var shown: [Member] {
        let ranked = members.enumerated().sorted { a, b in
            let ra = a.element.state == .working ? 0 : 1, rb = b.element.state == .working ? 0 : 1
            return ra != rb ? ra < rb : a.offset < b.offset
        }
        return Array(ranked.prefix(3).map(\.element))
    }

    public var body: some View {
        let moving = working && !reduceMotion
        let minis = shown.map { CharacterLook(id: $0.id, kind: $0.kind) }
        TimelineView(.animation(paused: !moving)) { tl in
            let t = tl.date.timeIntervalSinceReferenceDate
            Canvas { ctx, _ in
                var c = ctx
                let m = CharacterView.margin
                c.scaleBy(x: size / 48, y: size / 48)
                c.translateBy(x: 48 * m, y: 48 * m) // room around the tile for a hop
                c.fill(Path(roundedRect: CGRect(x: 1, y: 1, width: 46, height: 46), cornerRadius: 15), with: .color(.primary.opacity(0.08)))
                if minis.isEmpty {
                    c.draw(Text("#").font(.system(size: 20, weight: .bold)).foregroundColor(.secondary), at: CGPoint(x: 24, y: 24))
                }
                for (i, look) in minis.enumerated() {
                    var hop = CharacterMotion.Hop()
                    if moving {
                        hop = CharacterMotion.hop(at: t, delay: 0.2 * Double(i))
                        hop.dy *= 40 / Self.positions[i].width // the page moves a mini 3 units of the tile, not 3/40 of itself
                    }
                    CharacterArt.draw(&c, look: look, in: Self.positions[i], hop: hop,
                                      blink: moving ? CharacterMotion.blink(at: t) : 1)
                }
            }
        }
        .frame(width: size * (1 + 2 * CharacterView.margin), height: size * (1 + 2 * CharacterView.margin))
        .frame(width: size, height: size)
        .accessibilityElement()
        .accessibilityLabel(word)
        .accessibilityAddTraits(.isImage)
        .cornerDot(dot ?? needsYou, size: size)
    }
}
