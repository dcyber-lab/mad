#if os(macOS)
import AppKit
import SwiftUI

/// Whether a text field has the keyboard in a window: the first responder is a text (a field's editor, a text view).
///
/// The approval cards name Return and Esc as keys (⏎ allows, esc denies), as hidden buttons with `.keyboardShortcut`. AppKit
/// tries a window's key equivalents before the key reaches the first responder, so while the composer has the keyboard a
/// Return typed in it could allow what a card asks (on a create card, create an agent). The cards leave their shortcuts out
/// while `isText` is true, rather than guard the action: a guarded action would still take the keystroke from the composer.
enum MacTextFocus {
    static func isText(_ responder: NSResponder?) -> Bool { responder is NSText }
}

/// Watches the first responder of the window the view is in, and says whether it is a text.
private struct TextFocusWatcher: NSViewRepresentable {
    @Binding var textFocused: Bool

    func makeNSView(context: Context) -> WatchingView {
        let v = WatchingView()
        v.changed = { text in DispatchQueue.main.async { textFocused = text } }
        return v
    }

    func updateNSView(_ view: WatchingView, context: Context) {
        view.changed = { text in DispatchQueue.main.async { textFocused = text } }
    }

    final class WatchingView: NSView {
        var changed: (Bool) -> Void = { _ in }
        private var observation: NSKeyValueObservation?

        override func viewDidMoveToWindow() {
            observation = nil
            guard let window else { return }
            observation = window.observe(\.firstResponder, options: [.initial, .new]) { [weak self] w, _ in
                self?.changed(MacTextFocus.isText(w.firstResponder))
            }
        }
    }
}

extension View {
    /// Keeps `textFocused` true while a text in the window has the keyboard.
    func watchesTextFocus(_ textFocused: Binding<Bool>) -> some View {
        background(TextFocusWatcher(textFocused: textFocused).frame(width: 0, height: 0).accessibilityHidden(true))
    }
}
#endif
