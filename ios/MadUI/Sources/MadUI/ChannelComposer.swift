import PhotosUI
import SwiftUI
#if canImport(UIKit)
import UIKit
#elseif canImport(AppKit)
import AppKit
#endif

/// The one-line composer: ＋ for pictures, a field that grows, send. Pictures wait above it as
/// thumbnails the person can take out (six at most); what is being answered waits above them.
struct ChannelComposer: View {
    let placeholder: String
    let readOnly: Bool
    /// Who an @ can wake: typing @ offers them, as the web page does.
    let mentions: [ChannelMember]
    @Binding var replying: ChannelQuote?
    let send: @MainActor @Sendable (String, [ChannelDraftImage], Int?) async -> Bool

    @State private var text = {
        #if DEBUG
        UserDefaults.standard.string(forKey: "composerText") ?? "" // `-composerText "@a"`: to look at the @ list
        #else
        ""
        #endif
    }()
    @State private var images: [ChannelDraftImage] = []
    @State private var picking = false
    @State private var picked: [PhotosPickerItem] = []
    @State private var sending = false
    @State private var note: String?
    @State private var pick = 0
    @FocusState private var focused: Bool

    private var canSend: Bool {
        !sending && (!text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !images.isEmpty)
    }

    var body: some View {
        if readOnly {
            Text(L("这是两个 agent 之间的对话，只能看。"))
                .font(MadType.small).foregroundStyle(.secondary)
                .frame(maxWidth: .infinity).padding(12)
        } else {
            composer
        }
    }

    private var composer: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let q = replying { replyBar(q).padding(.horizontal, phoneSide) }
            if !images.isEmpty { DraftImageStrip(images: $images).padding(.horizontal, phoneSide) }
            field
            if let note { Text(note).font(MadType.small).foregroundStyle(Color.madDanger).padding(.horizontal, phoneSide) }
        }
        // no side padding of its own: the column the messages are in gives the edges (the phone's capsule has its own)
        .padding(.top, 8)
        #if os(macOS)
        .padding(.bottom, 22)
        #endif
        .pastesPictures(focused: focused) { add($0) }
        // the @ list floats over the messages, its foot 8pt above the composer: the messages keep their size
        .overlay(alignment: .topLeading) {
            if !matches.isEmpty {
                // A zero-height line along the composer's top edge: the list stands on it (its foot 8pt above the
                // edge) and grows upward over the messages, at its own height.
                Color.clear.frame(height: 0)
                    .overlay(alignment: .bottomLeading) {
                        mentionList.fixedSize(horizontal: false, vertical: true).padding(.bottom, 8).padding(.leading, max(0, phoneSide - 8))
                    }
            }
        }
        .photosPicker(isPresented: $picking, selection: $picked, maxSelectionCount: max(1, ChannelLimits.images - images.count), matching: .images)
        .onChange(of: picked) { _, items in loadPicked(items) }
        .onChange(of: text) { _, _ in pick = 0 }
    }

    /// How far the lines above the phone's capsule stand in from the screen's edge.
    private var phoneSide: CGFloat {
        #if os(iOS)
        24
        #else
        18 // inside the capsule's rounded ends
        #endif
    }

    /// ＋, the field and send. On the Mac they are one rounded bar with a hairline and a soft shadow, as in a chat app;
    /// on the phone three pieces.
    @ViewBuilder private var field: some View {
        #if os(macOS)
        // design v6: a floating capsule (54pt, the float fill, a soft shadow): + at the left, send at the right
        HStack(alignment: .bottom, spacing: 12) {
            plus
            textField.padding(.vertical, 7)
            sendButton
        }
        .padding(.leading, 10).padding(.trailing, 8).padding(.vertical, 9)
        .frame(minHeight: Mac.composerHeight)
        .background(Color.macFloatFill, in: RoundedRectangle(cornerRadius: Mac.composerHeight / 2, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Mac.composerHeight / 2, style: .continuous).strokeBorder(Color.madLine, lineWidth: 1))
        .macFloat()
        #else
        // Grok's floating capsule: a grey round + at the left, the field, a grey round send at the right
        PhoneComposerCapsule {
            plus
            textField.padding(.vertical, 8)
            sendButton
        }
        #endif
    }

    private var textField: some View {
        TextField(placeholder, text: $text, axis: .vertical)
            .lineLimit(1...5)
            .textFieldStyle(.plain)
            #if os(iOS)
            .font(Phone.message)
            #else
            .font(Mac.composerText)
            #endif
            .focused($focused)
            #if os(macOS)
            .onSubmit { submit() }
            .onKeyPress(.upArrow) { matches.isEmpty ? .ignored : { pick = (pick + matches.count - 1) % matches.count; return .handled }() }
            .onKeyPress(.downArrow) { matches.isEmpty ? .ignored : { pick = (pick + 1) % matches.count; return .handled }() }
            .onKeyPress(.tab) { matches.isEmpty ? .ignored : { choose(matches[min(pick, matches.count - 1)]); return .handled }() }
            .onKeyPress(.return) { matches.isEmpty ? .ignored : { choose(matches[min(pick, matches.count - 1)]); return .handled }() }
            .onKeyPress(.escape) { matches.isEmpty ? .ignored : { text += " "; return .handled }() }
            #endif
    }

    private var sendButton: some View {
        Button(action: submit) {
            Image(systemName: "arrow.up")
                .font(.system(size: 17, weight: .semibold))
                #if os(iOS)
                .foregroundStyle(canSend ? Color.madPaper : Color.madFg2)
                .frame(width: 42, height: 42)
                .background(canSend ? Color.madFg : Color.madRaise, in: Circle())
                #else
                .foregroundStyle(canSend ? Color.madPaper : Color.madFg2)
                .frame(width: 36, height: 36)
                .background(canSend ? Color.madFg : Color.madRaise, in: Circle())
                #endif
        }
        .buttonStyle(.plain)
        .disabled(!canSend)
        .accessibilityLabel("发送")
    }

    // MARK: @

    /// What is typed after the last @, when that @ starts a word and the word is not over; nil otherwise.
    private var mentionQuery: String? { ChannelMention.query(in: text) }

    /// The members the word could be: those whose name starts with it first, then those that contain it.
    private var matches: [ChannelMember] {
        guard let q = mentionQuery else { return [] }
        return ChannelMention.matches(for: q, in: mentions)
    }

    private func choose(_ m: ChannelMember) {
        text = ChannelMention.completing(text, with: m.name)
    }

    private var mentionList: some View {
        VStack(spacing: 0) {
            ForEach(Array(matches.enumerated()), id: \.element.id) { i, m in
                Button { choose(m) } label: {
                    HStack(spacing: 10) {
                        CharacterView(id: m.id, kind: m.kind, state: m.state, size: 24)
                        Text(m.name).font(MadType.rowLine).lineLimit(1)
                        if !m.role.isEmpty, m.role != m.name { Text(m.role).font(MadType.small).foregroundStyle(.secondary) }
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, 10).padding(.vertical, 7)
                    // the selected one as the sidebar's open row is drawn: a soft rounded rect, inset
                    .background(i == min(pick, matches.count - 1) ? Color.primary.opacity(0.08) : Color.clear,
                                in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                    .padding(.horizontal, 4)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.vertical, 4)
        .background(Color.chatComposer, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Color.chatLine))
        .shadow(color: .black.opacity(0.08), radius: 8, y: 2)
        .frame(maxWidth: 360, alignment: .leading)
        .accessibilityLabel("可以 @ 的成员")
    }

    private var plus: some View {
        Menu {
            Button { picking = true } label: { Label(L("照片"), systemImage: "photo") }
            #if os(iOS)
            Button { pasteFromPasteboard() } label: { Label(L("粘贴图片"), systemImage: "doc.on.clipboard") }
            #endif
        } label: {
            Image(systemName: "plus")
                #if os(iOS)
                .font(.system(size: 19, weight: .medium))
                .foregroundStyle(Color.madFg)
                .frame(width: 42, height: 42)
                .background(Color.madRaise, in: Circle())
                #else
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(Color.madFg)
                .frame(width: 34, height: 34)
                .background(Color.madRaise, in: Circle())
                #endif
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .menuIndicator(.hidden)
        .disabled(images.count >= ChannelLimits.images)
        .accessibilityLabel("添加图片")
    }

    private func replyBar(_ q: ChannelQuote) -> some View {
        HStack(spacing: 8) {
            Rectangle().fill(Color.chatBlue).frame(width: 2, height: 16)
            Text(Lf("回复 %@：%@", q.who, q.text)).font(MadType.small).foregroundStyle(.secondary).lineLimit(1)
            Spacer(minLength: 0)
            Button { replying = nil } label: { Image(systemName: "xmark.circle.fill").foregroundStyle(.secondary) }
                .buttonStyle(.plain).accessibilityLabel("不回复")
        }
        .padding(.horizontal, 4)
    }

    private func submit() {
        guard canSend else { return }
        let body = text.trimmingCharacters(in: .whitespacesAndNewlines)
        let attached = images
        let to = replying?.seq
        sending = true
        note = nil
        Task { @MainActor in
            let ok = await send(body, attached, to)
            sending = false
            if ok {
                text = ""
                images = []
                picked = []
                replying = nil
            } else {
                note = L("没有发出去：再试一次。")
            }
        }
    }

    /// Takes a picture's bytes: made a type the server keeps, and refused past six or too large.
    private func add(_ data: Data) {
        guard images.count < ChannelLimits.images else { note = Lf("一条消息最多 %lld 张图片。", ChannelLimits.images); return }
        guard let d = ChannelImage.draft(from: data) else { note = L("这张图片放不进去（不是图片，或太大）。"); return }
        images.append(d)
    }

    private func loadPicked(_ items: [PhotosPickerItem]) {
        guard !items.isEmpty else { return }
        Task { @MainActor in
            for item in items {
                if let data = try? await item.loadTransferable(type: Data.self) { add(data) }
            }
            picked = []
        }
    }

    #if os(iOS)
    private func pasteFromPasteboard() {
        guard let image = UIPasteboard.general.image, let data = image.pngData() else { note = L("剪贴板里没有图片。"); return }
        add(data)
    }
    #endif
}


/// Typing @ in the composer: the word being typed, who it could be, and the text with it completed.
enum ChannelMention {
    /// What is typed after the last @, when that @ starts a word and the word is not over; nil otherwise.
    static func query(in text: String) -> String? {
        guard let at = text.lastIndex(of: "@") else { return nil }
        if let before = text[..<at].last, !before.isWhitespace { return nil }
        let word = text[text.index(after: at)...]
        return word.contains(where: \.isWhitespace) ? nil : String(word)
    }

    /// The members the word could be: those whose name starts with it first, then those that contain it; a faded
    /// role (no agent behind it) is not offered. At most six.
    static func matches(for query: String, in members: [ChannelMember]) -> [ChannelMember] {
        let q = query.lowercased()
        let names = members.filter { !$0.dim }
        let starts = names.filter { $0.name.lowercased().hasPrefix(q) }
        let has = names.filter { !$0.name.lowercased().hasPrefix(q) && $0.name.lowercased().contains(q) }
        return Array((starts + has).prefix(6))
    }

    /// `text` with the word being typed replaced by `@name ` (unchanged when there is none).
    static func completing(_ text: String, with name: String) -> String {
        guard query(in: text) != nil, let at = text.lastIndex(of: "@") else { return text }
        return String(text[..<at]) + "@\(name) "
    }
}
