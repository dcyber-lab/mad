import SwiftUI

// The confirm card of a bot's request to create an agent or a team (the approved mockup, bot-creates), and mad's own result
// lines. Own file: the ask card's work is elsewhere (ConversationView).

private struct OpenCreatedKey: EnvironmentKey { static let defaultValue: (CreateLine.Target) -> Void = { _ in } }

extension EnvironmentValues {
    /// What 打开 on a create result line does (set by the conversation or the channel).
    var madOpenCreated: (CreateLine.Target) -> Void {
        get { self[OpenCreatedKey.self] }
        set { self[OpenCreatedKey.self] = newValue }
    }
}

/// Which button of a create card was tapped and is being answered.
enum CreateSending: Equatable {
    case allow, deny, send

    /// The words a tapped button shows while the Mac answers (so its place and size stay).
    var words: String {
        switch self {
        case .allow: L("允许中…")
        case .deny: L("拒绝中…")
        case .send: L("发送中…")
        }
    }
}

/// The acts of a create card as the ask card's chips (`AskChipLabel`, #171): 允许 filled, 拒绝, 改一下… dashed; with the field
/// open, 取消 and 发送 (filled, off while the field is empty). A tapped chip says what it is doing and pulses; the others dim.
struct CreateChips: View {
    enum Tap { case allow, deny, change, cancel, send }
    let card: CreateCard
    let sending: CreateSending?
    let changing: Bool
    let canSend: Bool
    let tap: (Tap) -> Void

    var body: some View {
        FlowLayout(spacing: 8) {
            if changing {
                chip(.cancel, "create.cancel", L("取消"))
                chip(.send, "create.send", L("发送"), filled: true, sendingWords: CreateSending.send.words, mine: .send, off: !canSend)
            } else {
                if card.allow != nil { chip(.allow, "create.allow", L("允许"), filled: true, sendingWords: CreateSending.allow.words, mine: .allow) }
                if card.deny != nil { chip(.deny, "create.deny", L("拒绝"), sendingWords: CreateSending.deny.words, mine: .deny) }
                if card.change != nil { chip(.change, "create.change", L("改一下…"), dashed: true) }
            }
        }
    }

    private func chip(_ t: Tap, _ id: String, _ words: String, filled: Bool = false, dashed: Bool = false,
                      sendingWords: String? = nil, mine: CreateSending? = nil, off: Bool = false) -> some View {
        let tapped = mine != nil && sending == mine
        return Button { tap(t) } label: {
            AskChipLabel(text: tapped ? (sendingWords ?? words) : words, filled: filled || tapped, dashed: dashed, pulsing: tapped, reserve: sendingWords)
        }
        .buttonStyle(.plain)
        .allowsHitTesting(sending == nil)
        .disabled(off)
        .opacity(off ? 0.45 : (sending != nil && !tapped ? 0.45 : 1))
        .accessibilityIdentifier(id)
    }
}

/// The spec box: a row each of a role and its model, or of 模型 and the model, 加入群聊 and the channel.
private struct CreateSpecBox: View {
    let rows: [CreateCard.Row]
    var mac = false

    var body: some View {
        if !rows.isEmpty {
            // the phone's box at the Mac's size: rows of 34pt at least, 1px lines between them
            VStack(spacing: 0) {
                ForEach(Array(rows.enumerated()), id: \.element.id) { i, r in
                    HStack(spacing: 8) {
                        Text(r.left).font(.system(size: mac ? 14.5 : 16)).foregroundStyle(Color.madFg).lineLimit(1)
                        if !r.tag.isEmpty { Text(r.tag).font(.system(size: mac ? 12 : 13)).foregroundStyle(Color.madFg3).lineLimit(1) }
                        Spacer(minLength: 8)
                        Text(r.right).font(.system(size: mac ? 13.5 : 15)).foregroundStyle(Color.madFg3).lineLimit(1).fixedSize()
                    }
                    .frame(minHeight: mac ? 34 : 40)
                    if i < rows.count - 1 { Rectangle().fill(Color.madLine).frame(height: 1) }
                }
            }
            .padding(.vertical, 2).padding(.horizontal, 14)
            .background(Color.madCode, in: RoundedRectangle(cornerRadius: mac ? 11 : 14, style: .continuous))
        }
    }
}

// MARK: the phone's sheet

#if os(iOS)
/// The phone's confirm card (design v6, as the approval's sheet): floating above the composer, 28pt corners; the head, the name
/// and its project, the spec box, the first task or message, 拒绝 | 允许 pills and 改一下… under them.
struct CreateSheet: View {
    let card: CreateCard
    /// The change field is open: the screen takes its composer away while it is (the card has its own field), so the card sits
    /// right above the keyboard.
    @Binding var changing: Bool
    let act: (String, String?) -> Void
    @State private var sending: CreateSending?
    @State private var words = ""
    @State private var contentHeight: CGFloat = 0
    /// The top of the keyboard, in screen points, while it is up.
    @State private var keyboardTop: CGFloat?
    @FocusState private var focused: Bool
    static let maxHeight: CGFloat = 420
    /// What the card gets above the screen's keyboard: the status bar and the head are 130 points, and a gap.
    static let headRoom: CGFloat = 130

    /// The card is at most 420 tall, and less when the keyboard is up: it scrolls inside itself then, and the head stays.
    private var maxHeight: CGFloat { min(Self.maxHeight, keyboardTop.map { max(160, $0 - Self.headRoom) } ?? Self.maxHeight) }
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        ScrollView {
            content.background(GeometryReader { Color.clear.preference(key: CreateHeightKey.self, value: $0.size.height) })
        }
        .scrollBounceBehavior(.basedOnSize)
        .frame(height: contentHeight > 0 ? min(contentHeight, maxHeight) : nil)
        .onPreferenceChange(CreateHeightKey.self) { contentHeight = $0 }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) { n in
            keyboardTop = (n.userInfo?[UIResponder.keyboardFrameEndUserInfoKey] as? CGRect)?.minY
        }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillHideNotification)) { _ in keyboardTop = nil }
        .task(id: sending) {
            guard sending != nil else { return }
            try? await Task.sleep(for: .seconds(8))
            sending = nil
        }
        .padding(.horizontal, 18).padding(.top, 18).padding(.bottom, 14)
        // the shadows are the card shape's, not every child's: the spec box inside stays flat
        .background {
            RoundedRectangle(cornerRadius: Phone.cardRadius, style: .continuous).fill(scheme == .dark ? Color.madRaise : Color.madPaper)
                .shadow(color: Color(red: 15 / 255, green: 18 / 255, blue: 30 / 255).opacity(0.16), radius: 20, x: 0, y: 10)
                .shadow(color: Color(red: 15 / 255, green: 18 / 255, blue: 30 / 255).opacity(0.08), radius: 1.5, x: 0, y: 1)
        }
        .padding(.horizontal, 12)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(card.accessibilityLabel)
        .accessibilityIdentifier("create.card")
        .onDisappear { changing = false }
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(card.head).font(.system(size: 17, weight: .semibold)).foregroundStyle(Color.madWait)
                    .lineLimit(1).minimumScaleFactor(0.8)
                Spacer(minLength: 8)
                if !card.waited.isEmpty { Text(card.waited).font(.system(size: 14)).foregroundStyle(Color.madFg3).lineLimit(1) }
            }
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Text(card.name).font(.system(size: 21, weight: .semibold)).foregroundStyle(Color.madFg).lineLimit(1)
                if !card.project.isEmpty { Text(card.project).font(.system(size: 15)).foregroundStyle(Color.madFg3).lineLimit(1).fixedSize() }
            }
            CreateSpecBox(rows: card.rows)
            if !card.first.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    Text(card.firstLabel).font(.system(size: 14)).foregroundStyle(Color.madFg3)
                    Text(card.first).font(.system(size: 16)).lineSpacing(3).foregroundStyle(Color.madFg)
                        .fixedSize(horizontal: false, vertical: true).textSelection(.enabled)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            if changing { changeField }
            CreateChips(card: card, sending: sending, changing: changing, canSend: !trimmed.isEmpty) { t in
                switch t {
                case .allow: if let a = card.allow { sending = .allow; act(a, nil) }
                case .deny: if let d = card.deny { sending = .deny; act(d, nil) }
                case .change: changing = true; focused = true
                case .cancel: changing = false; words = ""
                case .send: send()
                }
            }
        }
    }

    private var changeField: some View {
        TextField(L("告诉它要改什么"), text: $words, axis: .vertical)
            .lineLimit(1...4).textFieldStyle(.plain).font(.system(size: 17))
            .focused($focused)
            .padding(.horizontal, 14).padding(.vertical, 12)
            .background(Color.madRaise, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .disabled(sending != nil)
            .accessibilityIdentifier("create.field")
    }

    private var trimmed: String { words.trimmingCharacters(in: .whitespacesAndNewlines) }

    private func send() {
        guard let change = card.change, !trimmed.isEmpty else { return }
        sending = .send
        act(change, trimmed)
    }
}

#endif

private struct CreateHeightKey: PreferenceKey {
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
}

// MARK: the Mac's card

#if os(macOS)
/// The Mac's confirm card, the live ask card's look (`AskCardView`): a floating white card, the head in amber words, the body,
/// the chips with the keys named at the right (Return allows, Esc denies).
struct CreateCardView: View {
    let card: CreateCard
    let act: (String, String?) -> Void
    @State private var sending: CreateSending?
    @State private var changing = false
    @State private var words = ""
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(card.head).font(.system(size: 16, weight: .semibold)).foregroundStyle(Color.madWait)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 8)
                if !card.waited.isEmpty { Text(card.waited).font(.system(size: 14)).foregroundStyle(Color.madFg3) }
            }
            VStack(alignment: .leading, spacing: 10) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(card.name).font(.system(size: 18, weight: .semibold)).foregroundStyle(Color.madFg).lineLimit(1)
                    if !card.project.isEmpty { Text(card.project).font(.system(size: 14)).foregroundStyle(Color.madFg3).lineLimit(1) }
                }
                CreateSpecBox(rows: card.rows, mac: true)
                if !card.first.isEmpty {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(card.firstLabel).font(.system(size: 13)).foregroundStyle(Color.madFg3)
                        Text(card.first).font(MadType.detail).foregroundStyle(Color.madFg)
                            .fixedSize(horizontal: false, vertical: true).textSelection(.enabled)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            acts
        }
        .padding(.horizontal, 18)
        .padding(.top, 16)
        .padding(.bottom, 14)
        .fixedSize(horizontal: false, vertical: true)
        .background {
            RoundedRectangle(cornerRadius: Mac.cardRadius, style: .continuous).fill(Color.macFloatFill)
                .shadow(color: Color(red: 15 / 255, green: 18 / 255, blue: 30 / 255).opacity(0.12), radius: 17, x: 0, y: 10)
        }
        .overlay(RoundedRectangle(cornerRadius: Mac.cardRadius, style: .continuous).strokeBorder(Color.madLine, lineWidth: 1))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(card.accessibilityLabel)
        .accessibilityIdentifier("create.card")
        .task(id: sending) {
            guard sending != nil else { return }
            try? await Task.sleep(for: .seconds(8))
            sending = nil
        }
    }

    @ViewBuilder private var acts: some View {
        VStack(alignment: .leading, spacing: 10) {
            if changing {
                TextField(L("告诉它要改什么"), text: $words, axis: .vertical)
                    .lineLimit(1...4).textFieldStyle(.plain).font(MadType.detail).focused($focused)
                    .padding(.horizontal, 13).padding(.vertical, 9)
                    .background(Color.madRaise, in: RoundedRectangle(cornerRadius: 11, style: .continuous))
                    .disabled(sending != nil).onSubmit(send)
                    .accessibilityIdentifier("create.field")
            }
            HStack(alignment: .top, spacing: 8) {
                CreateChips(card: card, sending: sending, changing: changing, canSend: !trimmed.isEmpty) { t in
                    switch t {
                    case .allow: if let a = card.allow { sending = .allow; act(a, nil) }
                    case .deny: if let d = card.deny { sending = .deny; act(d, nil) }
                    case .change: changing = true; focused = true
                    case .cancel: changing = false; words = ""
                    case .send: send()
                    }
                }
                if !changing {
                    Spacer(minLength: 8)
                    HStack(spacing: 14) {
                        if card.allow != nil { hint("⏎", L("允许")) }
                        if card.deny != nil { hint("esc", L("拒绝")) }
                    }
                    .font(.system(size: 13.5)).foregroundStyle(Color.madFg3).fixedSize().frame(minHeight: 36).accessibilityHidden(true)
                }
            }
        }
        .background {
            // Return allows and Esc denies, as the hint says; with the field open Esc cancels it (a focused field takes its own Return first)
            if !changing, let allow = card.allow {
                Button("") { sending = .allow; act(allow, nil) }.keyboardShortcut(.defaultAction)
                    .opacity(0).frame(width: 0, height: 0).accessibilityHidden(true).disabled(sending != nil)
            }
            if !changing, let deny = card.deny {
                Button("") { sending = .deny; act(deny, nil) }.keyboardShortcut(.cancelAction)
                    .opacity(0).frame(width: 0, height: 0).accessibilityHidden(true).disabled(sending != nil)
            }
            if changing {
                Button("") { changing = false; words = "" }.keyboardShortcut(.cancelAction)
                    .opacity(0).frame(width: 0, height: 0).accessibilityHidden(true).disabled(sending != nil)
            }
        }
    }

    private func hint(_ key: String, _ words: String) -> some View {
        HStack(spacing: 5) {
            Text(key)
                .font(.system(size: 11, design: .monospaced))
                .padding(.horizontal, 5).padding(.vertical, 1)
                .overlay(RoundedRectangle(cornerRadius: 4, style: .continuous).strokeBorder(Color.madLine2, lineWidth: 1))
            Text(words)
        }
    }

    private var trimmed: String { words.trimmingCharacters(in: .whitespacesAndNewlines) }

    private func send() {
        guard let change = card.change, !trimmed.isEmpty, sending == nil else { return }
        sending = .send
        act(change, trimmed)
    }
}
#endif

// MARK: the result line

/// mad's own line after the person answered: a made thing's tile, "团队「X」建好了" and a 打开 pill (the whole line is a button),
/// the creating form (a spinner, no sub, no 打开), or one centred grey line.
public struct CreateLineView: View {
    let line: CreateLine
    let open: (CreateLine.Target) -> Void

    public init(line: CreateLine, open: @escaping (CreateLine.Target) -> Void) { self.line = line; self.open = open }

    #if os(iOS)
    private static let markSize: CGFloat = 40
    private static let title = Font.system(size: 16, weight: .semibold)
    private static let sub = Font.system(size: 14)
    private static let note = Font.system(size: 14)
    private static let pill: CGFloat = 34
    #else
    private static let markSize: CGFloat = 32
    private static let title = Font.mad(14, weight: .semibold)
    private static let sub = Font.mad(12.5)
    private static let note = Font.mad(12.5)
    private static let pill: CGFloat = 28
    #endif

    public var body: some View {
        switch line.phase {
        case .note:
            Text(line.title).font(Self.note).foregroundStyle(Color.madFg3).lineSpacing(3)
                .multilineTextAlignment(.center).frame(maxWidth: .infinity)
                .padding(.horizontal, 24).padding(.top, 16)
                .accessibilityIdentifier("create.note")
        case .creating:
            card {
                ProgressView().controlSize(.small).frame(width: Self.markSize, height: Self.markSize)
                Text(line.title).font(Self.title).foregroundStyle(Color.madFg).lineLimit(1)
                Spacer(minLength: 0)
            }
            .accessibilityIdentifier("create.result")
        case .created:
            if let target = line.target {
                Button { open(target) } label: { created(showsOpen: true) }
                    .buttonStyle(.plain)
                    .accessibilityLabel(line.accessibilityLabel)
                    .accessibilityIdentifier("create.result")
            } else {
                created(showsOpen: false).accessibilityIdentifier("create.result")
            }
        }
    }

    private func created(showsOpen: Bool) -> some View {
        card {
            mark
            VStack(alignment: .leading, spacing: 2) {
                Text(line.title).font(Self.title).foregroundStyle(Color.madFg).lineLimit(1)
                if !line.sub.isEmpty { Text(line.sub).font(Self.sub).foregroundStyle(Color.madFg3).lineLimit(1) }
            }
            Spacer(minLength: 0)
            if showsOpen {
                HStack(spacing: 2) {
                    Text(L("打开")).font(.system(size: 15, weight: .semibold)).foregroundStyle(Color.madFg)
                    Image(systemName: "chevron.right").font(.system(size: 13, weight: .semibold)).foregroundStyle(Color.madFg3)
                }
                .padding(.horizontal, 14).frame(height: Self.pill)
                .overlay(Capsule().strokeBorder(Color.madLine2, lineWidth: 1))
                .accessibilityIdentifier("create.open")
            }
        }
    }

    @ViewBuilder private var mark: some View {
        switch line.mark {
        case let .team(members)?:
            GroupTile(members: members.map { ($0.id, $0.kind, $0.state) }, size: Self.markSize, dot: false)
                .frame(width: Self.markSize, height: Self.markSize)
        case let .agent(id, kind, state)?:
            CharacterView(id: id, kind: kind, state: state, size: Self.markSize).frame(width: Self.markSize, height: Self.markSize)
        case nil:
            Color.clear.frame(width: Self.markSize, height: Self.markSize)
        }
    }

    /// The line's box: across the column, a 1px border, radius 20, madPaper.
    private func card<C: View>(@ViewBuilder _ content: () -> C) -> some View {
        HStack(spacing: 12) { content() }
            .padding(.leading, 14).padding(.trailing, 12).padding(.vertical, 12)
            .background(Color.madPaper, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 20, style: .continuous).strokeBorder(Color.madLine2, lineWidth: 1))
            #if os(macOS)
            .frame(maxWidth: 520, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
            #endif
            .padding(.horizontal, 16).padding(.top, 16)
    }
}
