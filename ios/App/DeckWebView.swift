import SwiftUI
import WebKit

/// The full mad page. It keeps the token from the URL fragment itself; the
/// data store is not persistent, so that copy dies with the view and the
/// Keychain stays the only durable one. Only the Mac's own origin loads in
/// the view; every other link opens in the system browser.
struct DeckWebView: UIViewRepresentable {
    let url: URL

    func makeCoordinator() -> Coordinator { Coordinator(origin: Origin(url)) }

    func makeUIView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .nonPersistent()
        let web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = context.coordinator
        web.uiDelegate = context.coordinator
        web.isOpaque = false
        web.backgroundColor = .systemBackground
        web.load(URLRequest(url: url))
        return web
    }

    func updateUIView(_ web: WKWebView, context: Context) {}

    struct Origin: Equatable {
        let scheme: String?
        let host: String?
        let port: Int

        init(_ url: URL) {
            scheme = url.scheme?.lowercased()
            host = url.host?.lowercased()
            port = url.port ?? (scheme == "http" ? 80 : 443)
        }
    }

    final class Coordinator: NSObject, WKNavigationDelegate, WKUIDelegate {
        let origin: Origin
        init(origin: Origin) { self.origin = origin }

        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                     decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            guard let url = action.request.url else { return decisionHandler(.cancel) }
            if Origin(url) == origin {
                decisionHandler(.allow)
            } else {
                decisionHandler(.cancel)
                openOutside(url)
            }
        }

        // window.open and target=_blank: never a new web view; outside
        // origins go to the system browser.
        func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                     for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
            if let url = action.request.url {
                if Origin(url) == origin { webView.load(action.request) } else { openOutside(url) }
            }
            return nil
        }

        private func openOutside(_ url: URL) {
            guard let s = url.scheme?.lowercased(), s == "https" || s == "http" else { return }
            UIApplication.shared.open(url)
        }
    }
}
