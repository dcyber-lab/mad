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

/// A tapped button pulses while the Mac answers (opacity 1 to 0.6, 1.1s, alternating); still, at 0.6, with Reduce Motion.
private struct CreatePulse: ViewModifier {
    let active: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var low = false

    func body(content: Content) -> some View {
        content
            .opacity(active && low ? 0.6 : 1)
            .onChange(of: active, initial: true) { _, on in
                guard on, !reduceMotion else { low = on; return }
                withAnimation(.easeInOut(duration: 1.1).repeatForever(autoreverses: true)) { low = true }
            }
    }
}

extension View {
    fileprivate func createPulse(_ active: Bool) -> some View { modifier(CreatePulse(active: active)) }
}

/// The spec box: a row each of a role and its model, or of 模型 and the model, 加入群聊 and the channel.
private struct CreateSpecBox: View {
    let rows: [CreateCard.Row]
    var mac = false

    var body: some View {
        if !rows.isEmpty {
            if mac {
                Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 3) {
                    ForEach(rows) { r in
                        GridRow {
                            HStack(spacing: 6) {
                                Text(r.left).foregroundStyle(Color.madFg).lineLimit(1)
                                if !r.tag.isEmpty { Text(r.tag).font(.mad(11.5)).foregroundStyle(Color.madFg3) }
                            }
                            Text(r.right).foregroundStyle(Color.madFg3).lineLimit(1)
                        }
                    }
                }
                .font(.mad(13))
                .frame(maxWidth: .infinity, alignment: .leading)
            } else {
                VStack(spacing: 0) {
                    ForEach(Array(rows.enumerated()), id: \.element.id) { i, r in
                        HStack(spacing: 8) {
                            Text(r.left).font(.system(size: 16)).foregroundStyle(Color.madFg).lineLimit(1)
                            if !r.tag.isEmpty { Text(r.tag).font(.system(size: 13)).foregroundStyle(Color.madFg3).lineLimit(1) }
                            Spacer(minLength: 8)
                            Text(r.right).font(.system(size: 15)).foregroundStyle(Color.madFg3).lineLimit(1).fixedSize()
                        }
                        .frame(minHeight: 40)
                        if i < rows.count - 1 { Rectangle().fill(Color.madLine).frame(height: 1) }
                    }
                }
                .padding(.vertical, 2).padding(.horizontal, 14)
                .background(Color.madCode, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            }
        }
    }
}

// MARK: the phone's sheet

#if os(iOS)
/// The phone's confirm card (design v6, as the approval's sheet): floating above the composer, 28pt corners; the head, the name
/// and its project, the spec box, the first task or message, 拒绝 | 允许 pills and 改一下… under them.
struct CreateSheet: View {
    let card: CreateCard
    let act: (String, String?) -> Void
    @State private var sending: CreateSending?
    @State private var changing = false
    @State private var words = ""
    @State private var contentHeight: CGFloat = 0
    @FocusState private var focused: Bool
    static let maxHeight: CGFloat = 420
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        ScrollView {
            content.background(GeometryReader { Color.clear.preference(key: CreateHeightKey.self, value: $0.size.height) })
        }
        .scrollBounceBehavior(.basedOnSize)
        .frame(height: contentHeight > 0 ? min(contentHeight, Self.maxHeight) : nil)
        .onPreferenceChange(CreateHeightKey.self) { contentHeight = $0 }
        .padding(.horizontal, 18).padding(.top, 18).padding(.bottom, 14)
        .background(scheme == .dark ? Color.madRaise : Color.madPaper, in: RoundedRectangle(cornerRadius: Phone.cardRadius, style: .continuous))
        .shadow(color: Color(red: 15 / 255, green: 18 / 255, blue: 30 / 255).opacity(0.16), radius: 20, x: 0, y: 10)
        .shadow(color: Color(red: 15 / 255, green: 18 / 255, blue: 30 / 255).opacity(0.08), radius: 1.5, x: 0, y: 1)
        .padding(.horizontal, 12)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(card.accessibilityLabel)
        .accessibilityIdentifier("create.card")
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
            pills
            if !changing, card.change != nil {
                Button { changing = true; focused = true } label: {
                    Text(L("改一下…")).font(.system(size: 15)).foregroundStyle(Color.madFg2)
                        .frame(maxWidth: .infinity, minHeight: 40).contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .disabled(sending != nil)
                .accessibilityIdentifier("create.change")
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

    private var pills: some View {
        HStack(spacing: 10) {
            if changing {
                Button { changing = false; words = "" } label: { Text(L("取消")) }
                    .buttonStyle(CreatePill(filled: false)).disabled(sending != nil)
                    .accessibilityIdentifier("create.cancel")
                Button { send() } label: { Text(sending == .send ? CreateSending.send.words : L("发送")) }
                    .buttonStyle(CreatePill(filled: true))
                    .disabled(sending != nil || trimmed.isEmpty)
                    .createPulse(sending == .send)
                    .accessibilityIdentifier("create.send")
            } else {
                if let deny = card.deny {
                    Button { sending = .deny; act(deny, nil) } label: { Text(sending == .deny ? CreateSending.deny.words : L("拒绝")) }
                        .buttonStyle(CreatePill(filled: false)).disabled(sending != nil)
                        .createPulse(sending == .deny)
                        .accessibilityIdentifier("create.deny")
                }
                if let allow = card.allow {
                    Button { sending = .allow; act(allow, nil) } label: { Text(sending == .allow ? CreateSending.allow.words : L("允许")) }
                        .buttonStyle(CreatePill(filled: true)).disabled(sending != nil)
                        .createPulse(sending == .allow)
                        .accessibilityIdentifier("create.allow")
                }
            }
        }
    }

    private var trimmed: String { words.trimmingCharacters(in: .whitespacesAndNewlines) }

    private func send() {
        guard let change = card.change, !trimmed.isEmpty else { return }
        sending = .send
        act(change, trimmed)
    }
}

/// A 50pt pill of the card: 允许 filled black, the others outlined; dimmed while another one is being answered.
private struct CreatePill: ButtonStyle {
    let filled: Bool
    @Environment(\.isEnabled) private var enabled
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 17, weight: .semibold)).lineLimit(1)
            .foregroundStyle(filled ? Color.madPaper : Color.madFg)
            .frame(maxWidth: .infinity, minHeight: 50).padding(.horizontal, 12)
            .background(filled ? Color.madFg : Color.madPaper, in: Capsule())
            .overlay(Capsule().strokeBorder(filled ? Color.madFg : Color.madLine2, lineWidth: 1))
            .opacity(configuration.isPressed ? 0.7 : 1)
    }
}
#endif

private struct CreateHeightKey: PreferenceKey {
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
}

// MARK: the Mac's card

#if os(macOS)
/// The Mac's confirm card (the ask card's look): the asker's avatar and the head in an amber bar, the body, the acts with the keys named
/// at the right (Return allows, Esc denies).
struct CreateCardView: View {
    let card: CreateCard
    let act: (String, String?) -> Void
    @State private var sending: CreateSending?
    @State private var changing = false
    @State private var words = ""
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                CharacterView(id: card.askerID, kind: card.askerKind, state: .needsYou, size: 18)
                Text(card.head).lineLimit(1)
                Spacer(minLength: 8)
                if !card.waited.isEmpty { Text(card.waited).font(.mad(11.5)).fontWeight(.regular).foregroundStyle(Color.madFg3) }
            }
            .font(.mad(12.5, weight: .semibold)).foregroundStyle(Color.madWait)
            .padding(.horizontal, 12).padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.madWaitBg)
            VStack(alignment: .leading, spacing: 8) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(card.name).font(.mad(15, weight: .semibold)).foregroundStyle(Color.madFg).lineLimit(1)
                    if !card.project.isEmpty { Text(card.project).font(.mad(13)).foregroundStyle(Color.madFg3).lineLimit(1) }
                }
                CreateSpecBox(rows: card.rows, mac: true)
                if !card.first.isEmpty {
                    (Text(card.firstLabel + " ").foregroundColor(.madFg3) + Text(card.first).foregroundColor(.madFg))
                        .font(.mad(13)).fixedSize(horizontal: false, vertical: true).textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            .padding(.horizontal, 12).padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .overlay(alignment: .bottom) { Rectangle().fill(Color.madLine).frame(height: 1) }
            acts.padding(.horizontal, 12).padding(.vertical, 9)
        }
        .background(Color.madPaper)
        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Color.madWait.opacity(0.45), lineWidth: 1))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(card.accessibilityLabel)
        .accessibilityIdentifier("create.card")
    }

    @ViewBuilder private var acts: some View {
        if changing {
            VStack(alignment: .leading, spacing: 8) {
                TextField(L("告诉它要改什么"), text: $words, axis: .vertical)
                    .lineLimit(1...4).textFieldStyle(.plain).font(.mad(13)).focused($focused)
                    .padding(.horizontal, 10).padding(.vertical, 6)
                    .background(Color.madRaise, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                    .disabled(sending != nil).onSubmit(send)
                    .accessibilityIdentifier("create.field")
                HStack(spacing: 8) {
                    Button(sending == .send ? CreateSending.send.words : L("发送"), action: send).buttonStyle(.mad(.primary))
                        .disabled(sending != nil || trimmed.isEmpty).createPulse(sending == .send)
                        .accessibilityIdentifier("create.send")
                    Button(L("取消")) { changing = false; words = "" }.buttonStyle(.mad())
                        .keyboardShortcut(.cancelAction).disabled(sending != nil)
                        .accessibilityIdentifier("create.cancel")
                }
            }
        } else {
            HStack(alignment: .center, spacing: 8) {
                if let allow = card.allow {
                    Button(sending == .allow ? CreateSending.allow.words : L("允许")) { sending = .allow; act(allow, nil) }
                        .buttonStyle(.mad(.primary)).disabled(sending != nil).createPulse(sending == .allow)
                        .accessibilityIdentifier("create.allow")
                }
                if let deny = card.deny {
                    Button(sending == .deny ? CreateSending.deny.words : L("拒绝")) { sending = .deny; act(deny, nil) }
                        .buttonStyle(.mad()).disabled(sending != nil).createPulse(sending == .deny)
                        .accessibilityIdentifier("create.deny")
                }
                if card.change != nil {
                    Button(L("改一下…")) { changing = true; focused = true }.buttonStyle(.mad()).disabled(sending != nil)
                        .accessibilityIdentifier("create.change")
                }
                Spacer(minLength: 8)
                HStack(spacing: 4) {
                    if card.allow != nil { Kbd("⏎"); Text(L("允许")) }
                    if card.deny != nil { Kbd("esc"); Text(L("拒绝")) }
                }
                .font(.mad(11.5)).foregroundStyle(Color.madFg3).fixedSize().accessibilityHidden(true)
            }
            .background {
                if let allow = card.allow {
                    Button("") { sending = .allow; act(allow, nil) }.keyboardShortcut(.defaultAction)
                        .opacity(0).frame(width: 0, height: 0).accessibilityHidden(true).disabled(sending != nil)
                }
                if let deny = card.deny {
                    Button("") { sending = .deny; act(deny, nil) }.keyboardShortcut(.cancelAction)
                        .opacity(0).frame(width: 0, height: 0).accessibilityHidden(true).disabled(sending != nil)
                }
            }
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
