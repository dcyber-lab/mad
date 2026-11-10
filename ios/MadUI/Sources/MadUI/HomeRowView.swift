import SwiftUI

/// The sizes a list row is drawn at: the Mac's sidebar (the design's 32pt avatar, 13.5pt name, 12pt line) or the
/// phone's home (50pt, 19pt and 16pt, as Grok's).
struct RowMetrics {
    var avatar: CGFloat
    var name: Font
    var line: Font
    var end: Font
    var spacing: CGFloat
    /// The phone's list draws the end (time over a marker) its own way.
    var isPhone = false

    /// Design v6 (docs/design/mac-v6.html): a 34pt picture, a 16pt name and a 14pt grey line, 12pt apart.
    static let mac = RowMetrics(avatar: 34, name: MadType.rowName, line: MadType.rowLine,
                                end: .system(size: 13), spacing: 12)
    #if os(iOS)
    /// Grok's measured home list: 50pt avatar, 19pt name, 16pt preview, 14pt time (see `Phone`).
    static let phone = RowMetrics(avatar: Phone.rowAvatar, name: Phone.rowName, line: Phone.rowPreview, end: Phone.rowTime,
                                  spacing: Phone.rowGap, isPhone: true)
    #endif
}

/// A row of the list: the avatar with its state ring (or the group's tile), the name, one line under it (the tool in
/// bold, then what it does or said), and at the right how long it has worked, a 完成 pill, or a channel's unread count.
struct HomeRowView: View {
    let row: HomeRow
    let metrics: RowMetrics
    /// The avatar's size at the default text size; it grows with Dynamic Type on the phone.
    @ScaledMetric private var size: CGFloat
    @State private var lit = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

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
            VStack(alignment: .leading, spacing: metrics.isPhone ? 1 : 2) {
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
        .background {
            // the soft highlight of a row a bot's request just made: madAccent at 7% fading to nothing in 1.2s, once
            // (none with Reduce Motion)
            RoundedRectangle(cornerRadius: 12, style: .continuous).fill(Color.madAccent.opacity(lit ? 0.07 : 0))
        }
        .task {
            guard row.fresh, !HomeFresh.isShown(row.id) else { return }
            HomeFresh.markShown(row.id)
            guard !reduceMotion else { return }
            lit = true
            withAnimation(.easeOut(duration: 1.2)) { lit = false }
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
        #if os(iOS)
        if metrics.isPhone { phoneEnd } else { macEnd }
        #else
        macEnd
        #endif
    }

    #if os(iOS)
    /// The phone's end: the time in light grey, and under it the unread dot or a 完成 pill.
    private var phoneEnd: some View {
        VStack(alignment: .trailing, spacing: 9) {
            if !row.age.isEmpty { Text(row.age).font(metrics.end).monospacedDigit().foregroundStyle(Color.madFg3).lineLimit(1).fixedSize() }
            if row.kind == .agent && row.state == .done {
                MadPill(text: L("完成"))
            } else if row.kind != .agent && row.unread > 0 {
                Circle().fill(Color.madAccent).frame(width: 10, height: 10)
                    .accessibilityHidden(true)
            }
        }
    }
    #endif

    @ViewBuilder private var macEnd: some View {
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
