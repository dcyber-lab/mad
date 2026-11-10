import MadUI
import SwiftUI

/// The page of a form in the detail pane (新群聊, 新团队): a title and a note, then the fields. On the iPhone it has a
/// head with the way back to the list; the Mac's detail pane has none.
struct FormPage<Content: View>: View {
    let title: String
    var note = ""
    var onBack: (() -> Void)?
    @ViewBuilder let content: Content

    #if os(macOS)
    private static var top: CGFloat { 72 }
    private static var side: CGFloat { 32 }
    #else
    private static var top: CGFloat { 28 }
    private static var side: CGFloat { 18 }
    #endif

    var body: some View {
        VStack(spacing: 0) {
            #if os(iOS)
            HStack {
                Button { onBack?() } label: {
                    Image(systemName: "chevron.backward").font(.system(size: 19, weight: .medium)).foregroundStyle(Color.madFg)
                        .frame(width: Phone.button, height: Phone.button)
                }
                .accessibilityLabel("返回")
                Spacer()
            }
            .padding(.horizontal, 8)
            #endif
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    Text(title).font(.system(size: 30, weight: .bold)).tracking(-0.4).foregroundStyle(Color.madFg)
                    if !note.isEmpty { Text(note).font(.system(size: 15)).foregroundStyle(Color.madFg3).padding(.top, -8) }
                    content
                }
                .padding(.top, Self.top)
                .padding(.bottom, 40)
                .frame(maxWidth: 640, alignment: .leading)
                .padding(.horizontal, Self.side)
                .frame(maxWidth: .infinity)
            }
            // the whole pane: a scroll view alone in the detail takes only its content's width
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .background(Color.madPaper)
    }
}

/// A field with its name over it.
struct FormField<Content: View>: View {
    let label: String
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(label).font(.system(size: 13, weight: .semibold)).foregroundStyle(Color.madFg3)
            content
        }
    }
}

extension View {
    /// A bordered box for a field's content.
    func formBox() -> some View {
        padding(.horizontal, 12).padding(.vertical, 9)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.madPaper, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Color.madLine, lineWidth: 1))
    }

    /// A text field's look: plain, in a box, never capitalized or corrected (names, branches).
    func formText() -> some View {
        #if os(iOS)
        textFieldStyle(.plain).font(.system(size: 15)).formBox().textInputAutocapitalization(.never).autocorrectionDisabled()
        #else
        textFieldStyle(.plain).font(.system(size: 15)).formBox().autocorrectionDisabled()
        #endif
    }
}

/// A choice among a few, as a box that opens a menu.
struct FormMenu: View {
    let options: [(value: String, label: String)]
    @Binding var selection: String

    var body: some View {
        Menu {
            ForEach(options, id: \.value) { o in
                Button { selection = o.value } label: {
                    if o.value == selection { Label(o.label, systemImage: "checkmark") } else { Text(o.label) }
                }
            }
        } label: {
            HStack(spacing: 6) {
                Text(options.first { $0.value == selection }?.label ?? selection)
                    .lineLimit(1).truncationMode(.tail)
                Spacer(minLength: 4)
                Image(systemName: "chevron.up.chevron.down").font(.system(size: 10, weight: .semibold)).foregroundStyle(Color.madFg3)
            }
            .font(.system(size: 15))
            .foregroundStyle(Color.madFg)
            .formBox()
            .contentShape(Rectangle())
        }
        .menuStyle(.button).buttonStyle(.plain).menuIndicator(.hidden)
    }
}

/// The form's one button; `busy` says what is going on while it works.
struct FormSubmit: View {
    let title: String
    var busy: String?
    var enabled = true
    let action: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            Button(action: action) {
                Text(title).font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(enabled && busy == nil ? Color.madPaper : Color.madFg3)
                    .padding(.horizontal, 22).frame(height: 38)
                    .background(enabled && busy == nil ? Color.madFg : Color.madRaise, in: Capsule())
            }
            .buttonStyle(.plain)
            .disabled(!enabled || busy != nil)
            if let busy { Text(busy).font(.system(size: 13)).foregroundStyle(Color.madFg3) }
        }
    }
}

/// Why the form was refused, under it.
struct FormError: View {
    let text: String?

    var body: some View {
        if let text { Text(text).font(.system(size: 14)).foregroundStyle(Color.madDanger).frame(maxWidth: .infinity, alignment: .leading) }
    }
}
