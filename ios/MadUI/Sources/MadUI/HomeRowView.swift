import SwiftUI

/// A row of the home list: the character (or the group's tile), the name and a one-line preview.
struct HomeRowView: View {
    let row: HomeRow
    /// The character's size at the default text size; it grows with Dynamic Type.
    @ScaledMetric private var size: CGFloat

    init(row: HomeRow, size: CGFloat) {
        self.row = row
        _size = ScaledMetric(wrappedValue: size, relativeTo: .body)
    }

    var body: some View {
        HStack(spacing: 14) {
            picture
                // a stopped character dims itself; a row dimmed for another reason dims it here
                .opacity(row.dimmed && row.state != .stopped ? 0.5 : 1)
            VStack(alignment: .leading, spacing: 1) {
                Text(row.title)
                    .font(.body.weight(.semibold))
                    .lineLimit(1)
                    .opacity(row.dimmed ? 0.5 : 1)
                if !row.preview.isEmpty {
                    Text(row.preview)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 0)
        }
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private var picture: some View {
        if row.kind == .agent {
            CharacterView(id: row.id, kind: row.agentKind, state: row.state, size: size, dot: row.needsYou)
        } else {
            GroupTile(members: row.tile, size: size, dot: row.needsYou)
        }
    }
}
