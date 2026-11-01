import SwiftUI

/// The one word under the person's latest message (option C): "未读", "3 个未读", "已读", "已回复"… It is as high as a
/// line from the moment the message shows (empty until a receipt comes), so nothing below it moves when the word
/// changes, and the word crossfades. A tap opens a small card of who and how far each got.
struct ReceiptLine: View {
    let receipt: ChannelReceipt?
    @State private var card = false

    /// The Mac's card hangs under the line. On the phone the line is near the foot of the screen, and a card under it would
    /// have the composer's strip to fit in: it stands above the line.
    private static var arrowEdge: Edge {
        #if os(macOS)
        .top
        #else
        .bottom
        #endif
    }

    var body: some View {
        let word = receipt?.word ?? ""
        // The word is its own view at its own width, right-aligned by the row around it: the card hangs on the word, and
        // its arrow points at it. The touch area is padded to 44pt and the padding taken off again, so the line keeps
        // its 20pt and nothing moves.
        HStack(spacing: 0) {
            Spacer(minLength: 0)
            Text(word)
                .font(MadType.small)
                .foregroundStyle(receipt?.isUnread == true ? Color.chatBlue : Color.secondary)
                .lineLimit(1)
                .contentTransition(.opacity)
                .animation(.easeInOut(duration: 0.15), value: word)
                .popover(isPresented: $card, arrowEdge: Self.arrowEdge) {
                    if let receipt { ReceiptCard(receipt: receipt).presentationCompactAdaptation(.popover) }
                }
                .padding(.vertical, 12).padding(.horizontal, 8)
                .contentShape(Rectangle())
                .onTapGesture { if receipt != nil { card = true } }
                .padding(.vertical, -12).padding(.horizontal, -8)
        }
        .padding(.trailing, 4)
        .frame(height: 20)
            #if DEBUG
            .task { // `-receiptCard YES`: opens the card a moment after the line shows, to look at it
                guard UserDefaults.standard.bool(forKey: "receiptCard"), receipt != nil else { return }
                try? await Task.sleep(for: .seconds(2))
                card = true
            }
            #endif
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(receipt?.spoken ?? "")
            .accessibilityAddTraits(receipt == nil ? [] : .isButton)
            .accessibilityAction { if receipt != nil { card = true } } // the default action: double-tap
            .accessibilityAction(named: "谁看了") { if receipt != nil { card = true } }
            .accessibilityHidden(receipt == nil)
    }
}

/// Who the message woke: each with its character, name and how far it got; and a footer.
struct ReceiptCard: View {
    let receipt: ChannelReceipt

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(receipt.targets) { t in
                HStack(spacing: 10) {
                    CharacterView(id: t.id, kind: t.kind, state: .idle, size: 22)
                    Text(t.name).font(MadType.small.weight(.semibold)).lineLimit(1)
                    Spacer(minLength: 12)
                    Text(ChannelReceipt.state(t.step)).font(MadType.small).foregroundStyle(.secondary)
                }
            }
            Text(receipt.footer).font(MadType.small).foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(14)
        .frame(minWidth: 240, maxWidth: 340, alignment: .leading)
        .fixedSize(horizontal: false, vertical: true)
    }
}
