import SwiftUI

/// Which of a channel's rows the list holds. A channel keeps hundreds of rows, and a lazy list of them opened at its bottom
/// guesses the heights of the rows above the screen, so the list holds a window of the last rows in a plain stack (exact
/// heights) and takes older ones as the person scrolls up. The window is named by its first row's id, not by a count from
/// the end: a message that arrives adds at the end and leaves the first row, and so what the person reads, where it is.
struct HeldWindow: Equatable {
    static let first = 40
    static let more = 40

    /// The first row held; nil is the last `first` rows.
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

    /// Takes `more` older rows: the window's first row moves back.
    mutating func earlier(in ids: [String]) {
        let i = max(0, start(in: ids) - Self.more)
        firstID = ids.isEmpty ? nil : ids[i]
    }
}

/// Where a scroll view is: how far it is scrolled and how tall its content is.
struct ScrollMetrics: Equatable {
    var offset: CGFloat
    var height: CGFloat
}

/// What `KeepsReadingPlace` remembers outside the view's state (reading it must not draw the view again).
final class PlaceMemory {
    var last = ScrollMetrics(offset: 0, height: 0)
    var before: ScrollMetrics?
    var askedAt = Date.distantPast
    var atEnd = false

    /// Older rows are about to be held: what the list measures now is what it was before they came. (Said before the window
    /// moves, not seen in the window's change: the new height may be measured in the same update.)
    func expectRows(atEnd: Bool) {
        before = last
        askedAt = Date()
        self.atEnd = atEnd
    }
}

/// Rows put in above what the person reads push it down by their height. When older rows are about to be held
/// (`memory.expectRows`), the content that grows within a moment is taken as those rows, and the offset moves by the same height:
/// the row being read does not move. A list at its end keeps its bottom by itself (`atEnd`), so it is left alone.
/// `idle` says whether the scroll is at rest: a jump of the offset while a finger or a flick moves the list is lost or
/// stops it, so the window moves only at rest.
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
                ScrollMetrics(offset: g.contentOffset.y + g.contentInsets.top, height: g.contentSize.height)
            } action: { _, m in
                if let before = memory.before, Date().timeIntervalSince(memory.askedAt) < 0.6, m.height != before.height {
                    memory.before = nil
                    if !memory.atEnd { position.scrollTo(y: m.offset + (m.height - before.height)) }
                }
                memory.last = m
            }
    }
}

extension View {
    /// The row being read stays where it is when older rows are held above it (iOS 18, macOS 15; see `KeepsReadingPlace`).
    @ViewBuilder func keepsReadingPlace(_ memory: PlaceMemory, idle: Binding<Bool>) -> some View {
        if #available(iOS 18, macOS 15, *) { modifier(KeepsReadingPlace(memory: memory, idle: idle)) } else { self }
    }
}

/// The top of the held rows, kept without drawing the view again.
final class TopWatch { var y: CGFloat = -.infinity }
