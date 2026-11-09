import SwiftUI

/// The sizes a list row is drawn at: the Mac's sidebar (the design's 32pt avatar, 13.5pt name, 12pt line) or the
/// phone's home (44pt, 16pt and 13pt, growing with Dynamic Type).
struct RowMetrics {
    var avatar: CGFloat
    var name: Font
    var line: Font
    var end: Font
    var spacing: CGFloat

    static let mac = RowMetrics(avatar: 32, name: .system(size: 13.5, weight: .medium), line: .system(size: 12),
                                end: .system(size: 11), spacing: 10)
    static let phone = RowMetrics(avatar: 44, name: .callout.weight(.semibold), line: .footnote, end: .caption, spacing: 12)
}

/// A row of the list: the avatar with its state ring (or the group's tile), the name, one line under it (the tool in
/// bold, then what it does or said), and at the right how long it has worked, a 完成 pill, or a channel's unread count.
struct HomeRowView: View {
    let row: HomeRow
    let metrics: RowMetrics
    /// The avatar's size at the default text size; it grows with Dynamic Type on the phone.
    @ScaledMetric private var size: CGFloat

    init(row: HomeRow, metrics: RowMetrics) {
        self.row = row
        self.metrics = metrics
        #if os(iOS)
        _size = ScaledMetric(wrappedValue: metrics.avatar, relativeTo: .body)
        #else
        _size = ScaledMetric(wrappedValue: metrics.avatar)
        #endif
    }

    var body: some View {
        HStack(spacing: metrics.spacing) {
            picture
                // a stopped character fades itself; a row dimmed for another reason (asleep) is faded here
                .opacity(row.dimmed && row.state != .stopped ? 0.5 : 1)
            VStack(alignment: .leading, spacing: 1) {
                Text(row.title)
                    .font(metrics.name)
                    .foregroundStyle(Color.madFg)
                    .lineLimit(1)
                if !row.tool.isEmpty || !row.preview.isEmpty {
                    line
                        .font(metrics.line)
                        .lineLimit(1)
                        .truncationMode(.tail)
                }
            }
            .opacity(row.dimmed ? 0.55 : 1)
            Spacer(minLength: 0)
            end
        }
        .contentShape(Rectangle())
        .accessibilityElement(children: .ignore) // one label, built by the row: the character's own would repeat it
        .accessibilityLabel(row.accessibilityLabel)
    }

    /// "**Bash** go test ./...": the tool in bold and the stronger grey, the rest in the secondary grey.
    private var line: Text {
        let rest = Text(row.preview).foregroundColor(.madFg3)
        if row.tool.isEmpty { return rest }
        let tool = Text(row.tool).fontWeight(.semibold).foregroundColor(.madFg2)
        return row.preview.isEmpty ? tool : tool + Text(" ") + rest
    }

    @ViewBuilder private var end: some View {
        if row.kind == .agent && row.state == .done {
            MadPill(text: L("完成"))
        } else if row.kind != .agent && row.unread > 0 {
            MadPill(text: "\(row.unread)", color: .white, fill: .madAccent)
        } else if !row.age.isEmpty {
            Text(row.age).font(metrics.end).monospacedDigit().foregroundStyle(Color.madFg3).lineLimit(1).fixedSize()
        }
    }

    @ViewBuilder private var picture: some View {
        if row.kind == .agent {
            CharacterView(id: row.id, kind: row.agentKind, state: row.state, size: size, dot: row.needsYou)
        } else {
            GroupTile(members: row.tile, size: size, dot: row.needsYou)
        }
    }
}
