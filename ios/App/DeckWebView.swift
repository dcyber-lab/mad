import SwiftUI

/// Shows the deck page's web view, which the model keeps alive.
struct DeckWebView: UIViewRepresentable {
    let host: DeckWebHost

    func makeUIView(context: Context) -> UIView { host.view }
    func updateUIView(_ view: UIView, context: Context) {}
}
