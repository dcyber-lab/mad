import SwiftUI

#if DEBUG
/// Every shape in every state, every face, and group tiles of 1, 2, 3 and 5
/// members: for looking at the characters, and for screenshots.
public struct CharacterGallery: View {
    public init() {}

    private let size: CGFloat = 52

    private func look(_ i: Int) -> CharacterLook { CharacterLook(shape: i, face: i % 5, color: i) }

    private func members(_ n: Int, _ states: [CharacterState]) -> [GroupTile.Member] {
        (0..<n).map { (id: "agent-\($0 + 3)", kind: "claude", state: states[$0 % states.count]) }
    }

    public var body: some View {
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
                    ForEach(0..<5, id: \.self) { CharacterView(look: CharacterLook(shape: 0, face: $0, color: $0 + 1), state: .idle, size: size) }
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
        .background(RedDot.defaultRing)
    }
}

#Preview { CharacterGallery() }
#endif
