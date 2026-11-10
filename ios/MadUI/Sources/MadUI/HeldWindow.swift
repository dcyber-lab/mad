import SwiftUI

/// Which of a channel's rows the list holds. A channel keeps hundreds of rows, and a lazy list of them opened at its bottom
/// guesses the heights of the rows above the screen, so the list holds a window of the last rows in a plain stack (exact
/// heights) and takes older ones as the person scrolls up. The window is named by its first row's id, not by a count from
/// the end: a message that arrives adds at the end and leaves the first row, and so what the person reads, where it is.
struct HeldWindow: Equatable {
    static let first = 40
    static let more = 40

    /// The first row held. nil only before the window is first shown (`pin`): the last `first` rows.
    var firstID: String?

    /// Where the window starts in `ids`: at `firstID`, or, when that is nil or gone (the row left the channel), `first` from the end.
    func start(in ids: [String]) -> Int {
        if let id = firstID, let i = ids.firstIndex(of: id) { return i }
        return max(0, ids.count - Self.first)
    }

    func items<T: Identifiable>(_ all: [T]) -> ArraySlice<T> where T.ID == String {
        all[start(in: all.map(\.id))...]
    }

    func holdsAll(_ ids: [String]) -> Bool { start(in: ids) == 0 }

    /// Names the first row, so that rows arriving at the end never move it. Done when the window is first shown, and again
    /// when its first row is gone; a reader at the end whose window has grown past twice `first` (messages kept coming) gets
    /// the last `first` rows again, so the stack stays short.
    mutating func pin(in ids: [String], atEnd: Bool) {
        guard !ids.isEmpty else { return }
        let s = start(in: ids)
        let trim = atEnd && ids.count - s > Self.first * 2
        if firstID == nil || !ids.contains(firstID!) || trim { firstID = ids[trim ? ids.count - Self.first : s] }
    }

    /// Takes `more` older rows: the window's first row moves back.
    mutating func earlier(in ids: [String]) {
        let i = max(0, start(in: ids) - Self.more)
        firstID = ids.isEmpty ? nil : ids[i]
    }
}

/// Where a scroll view is: how far it is scrolled (in content coordinates).
struct ScrollMetrics: Equatable {
    var offset: CGFloat
}

/// What `KeepsReadingPlace` remembers outside the view's state (reading it must not draw the view again).
final class PlaceMemory {
    /// The scroll offset now.
    var offset: CGFloat = 0
    /// The row that was the window's first before older rows were held: its top, in the stack's own space, is how much was put
    /// in above it. nil when no load waits for its measure.
    var anchorID: String?
    var askedAt = Date.distantPast
    var atEnd = false
    /// Moves the offset by this much (set by the modifier).
    var shift: (CGFloat) -> Void = { _ in }

    /// Older rows are about to be held above the row `id`, which is the window's first now.
    func expectRows(above id: String?, atEnd: Bool) {
        anchorID = id
        askedAt = Date()
        self.atEnd = atEnd
    }

    /// The stack's own space: its top is 0, and the first thing in it is the top sentinel, `sentinel` tall.
    static let space = "heldStack"
    static let sentinel: CGFloat = 1

    /// The anchor row's top in the stack's space is known: the rows put in above it are the height between the sentinel and it.
    func anchorMeasured(_ id: String, top: CGFloat) {
        guard anchorID == id else { return }
        let added = top - Self.sentinel
        guard added > 0.5, Date().timeIntervalSince(askedAt) < 1.5 else { return }
        anchorID = nil
        if !atEnd { shift(added) }
    }
}

/// Rows put in above what the person reads push it down by their height. The row that was the window's first is measured
/// after the rows went in above it (`PlaceMemory.anchorMeasured`: its top in the stack, less the sentinel), and the offset
/// moves by that: the row being read does not move, whatever else changed meanwhile (a message at the end, a picture). A
/// list at its end keeps its bottom by itself. `idle` says whether the scroll is at rest: a jump of the offset while a finger
/// or a flick moves the list is lost or stops it, so the window moves only at rest.
@available(iOS 18, macOS 15, *)
struct KeepsReadingPlace: ViewModifier {
    let memory: PlaceMemory
    @Binding var idle: Bool
    @State private var position = ScrollPosition()

    func body(content: Content) -> some View {
        content
            .scrollPosition($position)
            .onScrollPhaseChange { _, phase in idle = phase == .idle }
            .onScrollGeometryChange(for: ScrollMetrics.self) { g in
                ScrollMetrics(offset: g.contentOffset.y + g.contentInsets.top)
            } action: { _, m in memory.offset = m.offset }
            .onAppear { memory.shift = { added in position.scrollTo(y: memory.offset + added) } }
    }
}

extension View {
    /// The row being read stays where it is when older rows are held above it (iOS 18, macOS 15; see `KeepsReadingPlace`).
    @ViewBuilder func keepsReadingPlace(_ memory: PlaceMemory, idle: Binding<Bool>) -> some View {
        if #available(iOS 18, macOS 15, *) { modifier(KeepsReadingPlace(memory: memory, idle: idle)) } else { self }
    }
}

/// The chip before iOS 18 (and macOS 15), which cannot hold the reading place by itself nor tell a rest: a centred capsule at
/// the top of what is held, and the older rows come when it is tapped.
struct EarlierChip: View {
    let action: () -> Void
    var body: some View {
        Button(action: action) {
            Text(L("显示更早的消息")).font(.system(size: 15)).foregroundStyle(Color.madFg2)
                .padding(.horizontal, 16).frame(minHeight: 36)
                .overlay(Capsule().strokeBorder(Color.madLine2, lineWidth: 1))
                .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .frame(maxWidth: .infinity).padding(.vertical, 8)
    }
}

/// The top of the held rows, kept without drawing the view again.
final class TopWatch { var y: CGFloat = -.infinity }
