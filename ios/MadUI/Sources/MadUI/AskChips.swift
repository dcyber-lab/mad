import SwiftUI

/// The acts of an ask card as chips that wrap (the page's card, #163): about 36pt, at the chat's type size. A permission's
/// Allow, and a question's primary option, are the filled chips; Reply and the acts that take words are dashed. The chip
/// tapped keeps its words and pulses while the Mac is answering (an Allow or Deny says 允许中… / 拒绝中…), the others dim,
/// and the card does not change size. Shared by the iPhone's sheet and the Mac's card.
struct AskChips: View {
    let card: AskCard
    /// Called for an act that sends at once; one that takes words goes to `input`.
    let send: (AskAct) -> Void
    let input: (AskAct) -> Void
    /// The act sent and waiting for the Mac's answer; the card goes when it has answered. It is let go of after a while,
    /// so a card the Mac refused is not left dead.
    @State private var sent: String?

    static let font: Font = {
        #if os(iOS)
        Phone.message
        #else
        MadType.detail
        #endif
    }()

    var body: some View {
        FlowLayout(spacing: 8) {
            ForEach(card.acts) { a in
                Button { tap(a) } label: { chip(a) }
                    .buttonStyle(.plain)
                    .disabled(sent != nil)
            }
        }
        .task(id: sent) {
            guard sent != nil else { return }
            try? await Task.sleep(for: .seconds(8))
            sent = nil
        }
    }

    private func tap(_ a: AskAct) {
        if a.input { input(a); return }
        sent = a.id
        send(a)
    }

    /// Whether the chip is the filled one: an Allow, or the card's primary option (not Reply, which takes words).
    static func filled(_ a: AskAct) -> Bool { (a.primary && !a.input) || a.kind == .allow }

    /// The words on the chip: the act's own, or what it is doing once it is sent (an option keeps its words).
    static func words(_ a: AskAct, sending: Bool) -> String {
        guard sending else { return a.label }
        switch a.kind {
        case .allow: return L("允许中…")
        case .deny: return L("拒绝中…")
        default: return a.label
        }
    }

    private func chip(_ a: AskAct) -> some View {
        let mine = sent == a.id
        let filled = Self.filled(a) || mine
        // an Allow or a Deny is as wide as its sending words, so the card does not move when it is tapped
        let reserve = a.kind == .allow || a.kind == .deny ? Self.words(a, sending: true) : nil
        return AskChipLabel(text: Self.words(a, sending: mine), filled: filled, dashed: a.input, pulsing: mine, reserve: reserve)
            .opacity(sent != nil && !mine ? 0.45 : 1)
    }
}

/// One chip: the words in a capsule, 36pt at least, wrapping inside when longer than the line.
struct AskChipLabel: View {
    let text: String
    var filled = false
    var dashed = false
    var pulsing = false
    /// Other words the chip has to be wide enough for.
    var reserve: String?
    @State private var dim = false

    var body: some View {
        // the reserved words are laid out and drawn to nothing: the chip is as wide as the wider of the two
        ZStack(alignment: .leading) {
            if let reserve { Text(reserve).hidden() }
            Text(text)
        }
            .font(AskChips.font)
            .fontWeight(filled ? .medium : .regular)
            .multilineTextAlignment(.leading)
            .fixedSize(horizontal: false, vertical: true)
            .foregroundStyle(filled ? Color.madPaper : (dashed ? Color.madFg2 : Color.madFg))
            .padding(.horizontal, 14).padding(.vertical, 6)
            .frame(minWidth: filled ? 72 : 0, minHeight: 36)
            .background(filled ? Color.madFg : Color.clear, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 18, style: .continuous)
                .strokeBorder(filled ? Color.clear : Color.madLine2, style: StrokeStyle(lineWidth: 1, dash: dashed ? [4, 3] : [])))
            .contentShape(RoundedRectangle(cornerRadius: 18, style: .continuous))
            .opacity(pulsing && dim ? 0.6 : 1)
            .onChange(of: pulsing, initial: true) { _, on in
                guard on else { dim = false; return }
                withAnimation(.easeInOut(duration: 1.1).repeatForever(autoreverses: true)) { dim = true }
            }
    }
}

/// A form of several questions: each has its own chips to pick one from; 发送 is on once every question has a pick.
struct AskForm: View {
    let questions: [AskQuestion]
    let submit: ([String]) -> Void
    @State private var picks: [String: String] = [:]

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            ForEach(questions) { q in
                VStack(alignment: .leading, spacing: 8) {
                    Text(q.text).font(AskChips.font).foregroundStyle(Color.madFg)
                    FlowLayout(spacing: 8) {
                        ForEach(q.options, id: \.self) { o in
                            Button { picks[q.id] = o } label: { AskChipLabel(text: o, filled: picks[q.id] == o) }
                                .buttonStyle(.plain)
                        }
                    }
                }
            }
            Button { submit(questions.compactMap { picks[$0.id] }) } label: {
                AskChipLabel(text: L("发送"), filled: complete)
            }
            .buttonStyle(.plain)
            .disabled(!complete)
            .opacity(complete ? 1 : 0.45)
        }
    }

    var complete: Bool { questions.allSatisfy { picks[$0.id] != nil } }
}
