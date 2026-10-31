import SwiftUI

#if DEBUG
/// Every shape in every state, every face, 16 working bots, and group tiles of 1, 2, 3 and 5
/// members: for looking at the characters, and for screenshots.
public struct CharacterGallery: View {
    public init() {}

    private let size: CGFloat = 52

    private func look(_ i: Int) -> CharacterLook { CharacterLook(shape: i, color: (i * 5 + 2) % 8, key: "gallery-\(i)") }

    private func members(_ n: Int, _ states: [CharacterState]) -> [GroupTile.Member] {
        (0..<n).map { (id: "agent-\($0 + 3)", kind: "claude", state: states[$0 % states.count]) }
    }

    public var body: some View {
        ScrollViewReader { proxy in
        ScrollView([.vertical, .horizontal]) {
            VStack(alignment: .leading, spacing: 18) {
                Text("Shapes × states").font(.headline)
                Grid(horizontalSpacing: 18, verticalSpacing: 14) {
                    GridRow {
                        Text("")
                        ForEach(CharacterState.allCases, id: \.self) { Text($0.word).font(.caption).foregroundStyle(.secondary) }
                    }
                    ForEach(0..<8, id: \.self) { i in
                        GridRow {
                            Text(CharacterLook.shapes[i].name).font(.caption).foregroundStyle(.secondary)
                            ForEach(CharacterState.allCases, id: \.self) { CharacterView(look: look(i), state: $0, size: size) }
                        }
                    }
                }
                Text("Faces").font(.headline)
                HStack(spacing: 18) {
                    ForEach(Array(Expression.allCases.enumerated()), id: \.offset) { i, e in
                        VStack { StillFace(expression: e, index: i, size: size); Text(e.rawValue).font(.caption).foregroundStyle(.secondary) }
                    }
                }
                Text("16 different bots, working").font(.headline).id("bots")
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 84), spacing: 12)], alignment: .leading, spacing: 12) {
                    ForEach(0..<16, id: \.self) { i in
                        let id = "bot-" + String(i * 7919 + 13, radix: 36) + "-\(i)"
                        VStack {
                            CharacterView(id: id, kind: "claude", state: .working, size: size)
                            Text(CharacterPlan(key: id).style.rawValue).font(.caption2).foregroundStyle(.secondary)
                        }
                    }
                }
                Text("Group tiles: 1, 2, 3, 5 members (working, needs you, idle)").font(.headline)
                HStack(spacing: 18) {
                    GroupTile(members: members(1, [.working]), size: size)
                    GroupTile(members: members(2, [.working, .idle]), size: size)
                    GroupTile(members: members(3, [.idle, .needsYou, .working]), size: size)
                    GroupTile(members: members(5, [.idle, .idle, .idle, .idle, .working]), size: size)
                    GroupTile(members: members(0, [.idle]), size: size)
                }
                Text("Small and large").font(.headline)
                HStack(alignment: .bottom, spacing: 18) {
                    ForEach([28, 40, 46, 96], id: \.self) { CharacterView(id: "architect", kind: "claude", state: .working, size: CGFloat($0)) }
                    GroupTile(members: members(3, [.working, .idle, .idle]), size: 96)
                }
            }
            .padding(24)
        }
        // `-section bots` opens at the 16 working bots, for screenshots and recordings
        .onAppear { if let s = UserDefaults.standard.string(forKey: "section") { proxy.scrollTo(s, anchor: .top) } }
        }
        .background(RedDot.defaultRing)
    }
}

/// One face on a plain body, to see it still.
private struct StillFace: View {
    let expression: Expression
    let index: Int
    let size: CGFloat

    var body: some View {
        let look = CharacterLook(shape: 0, color: (index * 5 + 2) % 8, key: "face-\(expression.rawValue)")
        Canvas { ctx, _ in
            var c = ctx
            let m = CharacterView.margin
            CharacterArt.draw(&c, look: look, in: CGRect(x: size * m, y: size * m, width: size, height: size),
                              pose: CharacterPlan.Pose(face: expression))
        }
        .frame(width: size * (1 + 2 * CharacterView.margin), height: size * (1 + 2 * CharacterView.margin))
        .frame(width: size, height: size)
    }
}

#Preview { CharacterGallery() }
#endif
