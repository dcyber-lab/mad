import SwiftUI

/// The one word under the person's latest message (option C): "未读", "3 个未读", "已读", "已回复"… It is as high as a
/// line from the moment the message shows (empty until a receipt comes), so nothing below it moves when the word
/// changes, and the word crossfades. A tap opens a small card of who and how far each got.
struct ReceiptLine: View {
    let receipt: ChannelReceipt?
    @State private var card = {
        #if DEBUG
        UserDefaults.standard.bool(forKey: "receiptCard") // `-receiptCard YES`: to look at the card
        #else
        false
        #endif
    }()

    var body: some View {
        let word = receipt?.word ?? ""
        Text(word)
            .font(MadType.small)
            .foregroundStyle(receipt?.isUnread == true ? Color.chatBlue : Color.secondary)
            .lineLimit(1)
            .contentTransition(.opacity)
            .animation(.easeInOut(duration: 0.15), value: word)
            .frame(maxWidth: .infinity, alignment: .trailing)
            .padding(.trailing, 4)
            .frame(height: 20)
            .contentShape(Rectangle())
            .onTapGesture { if receipt != nil { card = true } }
            .popover(isPresented: $card, arrowEdge: .top) {
                if let receipt { ReceiptCard(receipt: receipt).presentationCompactAdaptation(.popover) }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(receipt?.spoken ?? "")
            .accessibilityAddTraits(receipt == nil ? [] : .isButton)
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
    }
}
