import SwiftUI

/// The type scale of the native views, one set for all of them. On the Mac SwiftUI's defaults (13pt body,
/// 10 to 11pt labels) read too small in a window, so each size is chosen; on the iPhone the text styles
/// stay as they were (and grow with Dynamic Type).
enum MadType {
    #if os(macOS)
    /// A row's or an agent's name.
    static let rowName = Font.system(size: 15, weight: .medium)
    /// A row's last line.
    static let rowLine = Font.mad(13)
    /// What is said: a message, what the person types.
    static let message = Font.system(size: 15)
    /// The opened text of a folded line, a card's body.
    static let detail = Font.system(size: 15)
    /// The handoff / done line.
    static let pill = Font.mad(14)
    /// A head's title.
    static let title = Font.system(size: 17, weight: .semibold)
    /// Sender names, the head's sub line, times, section titles.
    static let label = Font.mad(13)
    /// Quotes, notes, a row's small line.
    static let small = Font.mad(13)
    /// The smallest: a tag.
    static let tag = Font.mad(12)
    /// The extra space between the lines of what is said (15pt text on a 22 to 23pt pitch).
    static let messageLineSpacing: CGFloat = 4
    /// Code in a bubble, and in a tool's input and result.
    static let code = Font.mad(13, design: .monospaced)
    static let codeSmall = Font.mad(12, design: .monospaced)
    #else
    static let rowName = Font.body.weight(.semibold)
    static let rowLine = Font.subheadline
    static let message = Font.body
    static let detail = Font.callout
    static let pill = Font.subheadline
    static let title = Font.headline
    static let label = Font.caption
    static let small = Font.footnote
    static let tag = Font.caption2
    static let messageLineSpacing: CGFloat = 0
    static let code = Font.system(.footnote, design: .monospaced)
    static let codeSmall = Font.system(.caption, design: .monospaced)
    #endif
}

/// The message column of a channel or a conversation: every row and the composer share its two edges.
/// The avatar sits in the gutter on the left; others' bubbles start after it, mine end at the right edge,
/// and a bubble is as wide as its text up to a share of the column.
enum MadColumn {
    #if os(macOS)
    static let maxWidth: CGFloat = 960
    static let side: CGFloat = 32
    /// No gutter on the Mac: a sender's small character sits by its name above the bubbles.
    static let gutter: CGFloat = 0
    /// The most of the column a bubble takes.
    static let bubbleShare: CGFloat = 0.58
    #else
    static let maxWidth: CGFloat = 720
    static let side: CGFloat = 16
    /// The avatar (26) and the space after it.
    static let gutter: CGFloat = 36
    static let bubbleShare: CGFloat = 0.86
    #endif

    /// The widest a bubble may be in a column `width` wide.
    static func bubble(in width: CGFloat) -> CGFloat { max(120, (width - gutter) * bubbleShare) }
}

private struct ColumnWidthKey: EnvironmentKey { static let defaultValue: CGFloat = 0 }

extension EnvironmentValues {
    /// The width of the message column the view is drawn in (0 before it is measured).
    var madColumnWidth: CGFloat {
        get { self[ColumnWidthKey.self] }
        set { self[ColumnWidthKey.self] = newValue }
    }
}

/// Lays out its one child no wider than `maxWidth`, and as wide as the child needs up to that: a bubble
/// hugs its text. (`frame(maxWidth:)` would fill up to the width instead.)
struct MaxWidthLayout: Layout {
    var maxWidth: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        guard let child = subviews.first else { return .zero }
        return child.sizeThatFits(ProposedViewSize(width: min(proposal.width ?? maxWidth, maxWidth), height: proposal.height))
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        guard let child = subviews.first else { return }
        child.place(at: bounds.origin, anchor: .topLeading, proposal: ProposedViewSize(width: bounds.width, height: bounds.height))
    }
}

extension View {
    /// As wide as its content needs, up to `maxWidth`.
    func hugging(maxWidth: CGFloat) -> some View { MaxWidthLayout(maxWidth: maxWidth) { self } }
}

#if os(macOS)
// What the Mac's list uses to stay at its bottom as the person resizes the window. The iPhone's list is full-bleed and
// needs none of it: the keyboard changes its inset, not its size.
/// A list that was at its bottom when its window was resized is at its bottom after: the view that leaves the
/// bottom within a moment of the resize is put back. (The marker row at the end of the list reports leaving.)
struct BottomKeeper {
    private var resizedAt = Date.distantPast
    private var leftAt = Date.distantPast

    /// The scroll view's size changed.
    mutating func resized() -> Bool { resizedAt = Date(); return close }
    /// The marker row at the end of the list left the screen.
    mutating func leftBottom() -> Bool { leftAt = Date(); return close }

    private var close: Bool { abs(resizedAt.timeIntervalSince(leftAt)) < 0.4 }
}

/// Whether the person is at the bottom of a list that follows new messages. A message that arrives pushes the marker at
/// the end out of view for a moment, and the list follows it: that is not the person scrolling away, so a marker that
/// leaves within `window` of a follow is judged once the window is over (still out: they did scroll; back: they did not).
struct BottomWatch {
    static let window: TimeInterval = 0.5

    private(set) var shown = true
    private var followedAt = Date.distantPast
    private var resizedAt = Date.distantPast
    private var leftAt = Date.distantPast

    enum Left: Equatable {
        /// The person has left the bottom.
        case now
        /// Look again after this many seconds: `stillLeft` then says.
        case recheck(after: TimeInterval)
    }

    mutating func appeared() { shown = true }

    /// The list followed a new message to the bottom.
    mutating func followed(at now: Date = Date()) { followedAt = now }

    /// The marker left the screen.
    mutating func disappeared(at now: Date = Date()) -> Left {
        shown = false
        leftAt = now
        let wait = Self.window - now.timeIntervalSince(followedAt)
        return wait > 0 ? .recheck(after: wait) : .now
    }

    /// The scroll view's size changed (the keyboard, the composer, the window).
    mutating func resized(at now: Date = Date()) { resizedAt = now }

    /// The marker is out, and it left within a moment of a size change: the resize moved it, not the person.
    var leftWithResize: Bool { !shown && abs(leftAt.timeIntervalSince(resizedAt)) < 0.4 }

    /// At a recheck: the marker is still out.
    var stillLeft: Bool { !shown }

    /// The list's last row is on screen but well above the viewport's foot: the offset was left past the end of the
    /// content (the viewport grew), and an empty band shows under the last message.
    static func pastEnd(markerShown: Bool, markerFoot: CGFloat, viewportHeight: CGFloat) -> Bool {
        markerShown && viewportHeight > 0 && markerFoot < viewportHeight - 40
    }
}

#endif

/// What the link to the Mac says when it is not good ("连不上这台 Mac。"), for the Mac's name pill to show in place of
/// its state line: one place the person already looks, and no second floating thing.
public struct MadLinkNote: Equatable, Sendable {
    public var text: String
    public var symbol: String
    public init(text: String, symbol: String) { self.text = text; self.symbol = symbol }
}

private struct LinkNoteKey: EnvironmentKey { static let defaultValue: MadLinkNote? = nil }

extension EnvironmentValues {
    /// Set by the window when the Mac cannot be reached, refuses the token or fails; nil when all is well.
    public var madLinkNote: MadLinkNote? {
        get { self[LinkNoteKey.self] }
        set { self[LinkNoteKey.self] = newValue }
    }
}

/// The second line of a name pill: how the channel or agent stands, or, when the link is bad, that.
struct PillSubline: View {
    let text: String
    /// The Mac's pill is one line: it says only what is wrong (the link), not how the channel stands.
    var onlyProblems = false
    @Environment(\.madLinkNote) private var note

    var body: some View {
        if let note {
            HStack(spacing: 4) {
                Image(systemName: note.symbol).font(.system(size: 11))
                Text(note.text).lineLimit(1).truncationMode(.tail)
            }
            .font(MadType.label)
            .foregroundStyle(.orange)
        } else if !text.isEmpty && !onlyProblems {
            Text(text).font(MadType.label).foregroundStyle(.secondary).lineLimit(1)
        }
    }
}

extension View {
    /// Keeps a scroll view at its bottom through size changes (the keyboard coming and going, the composer growing) as the
    /// system lays them out, so no frame has an empty band in it. Older systems have `BottomWatch`'s settle check instead.
    @ViewBuilder func pinnedToBottomThroughResizes() -> some View {
        if #available(iOS 18, macOS 15, *) { defaultScrollAnchor(.bottom, for: .sizeChanges) } else { self }
    }
}

extension Font {
    /// A size chosen for the Mac's window. On the Mac it is that size; on the iPhone it is the text style a reader
    /// expects there, at least 13pt, and it grows with Dynamic Type: the Mac's 11 to 13.5pt read too small in the
    /// hand.
    public static func mad(_ size: CGFloat, weight: Font.Weight = .regular, design: Font.Design = .default) -> Font {
        #if os(macOS)
        return .system(size: size, weight: weight, design: design)
        #else
        return .system(MadType.style(forMac: size), design: design, weight: weight)
        #endif
    }
}

extension MadType {
    /// The iPhone's text style for a size chosen on the Mac: under 12 the footnote (13), under 13 the subheadline
    /// (15), under 14 the callout (16), under 17 the body (17); bigger ones are titles, kept as they are.
    static func style(forMac size: CGFloat) -> Font.TextStyle {
        switch size {
        case ..<12: .footnote
        case ..<13: .subheadline
        case ..<14: .callout
        case ..<17: .body
        case ..<20: .headline
        case ..<22: .title3
        case ..<28: .title2
        default: .title
        }
    }
}
