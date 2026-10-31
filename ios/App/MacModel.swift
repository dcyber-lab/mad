import MadKit
import SwiftUI
import WebKit

/// One open Mac: the deck page in a web view, and nothing else. The page is
/// the whole app; this only hosts it.
@MainActor
final class MacModel: ObservableObject {
    let mac: Mac
    private(set) lazy var web = DeckWebHost(url: mac.embeddedDeckURL)

    init(mac: Mac) { self.mac = mac }

    /// The Mac's own name, asked once when it is opened, so the Macs list keeps
    /// up with a Mac that was renamed. nil when it does not say.
    func machineName() async -> String? {
        try? await MadClient(mac: mac).deck().machine
    }
}

/// Owns the deck page's WKWebView. Only the Mac's own origin loads in it and
/// may speak to the app; every other link opens in the system browser. The
/// data store is not persistent, so the page's copy of the token dies with the
/// view and the Keychain stays the only durable one.
@MainActor
final class DeckWebHost: NSObject, WKNavigationDelegate, WKUIDelegate {
    /// What the page asked of the app (`window.webkit.messageHandlers.mad`).
    var onMessage: (DeckMessage) -> Void = { _ in }

    let view: WKWebView
    fileprivate let origin: WebOrigin

    /// Fixes the page's scale at 1, whatever its viewport tag says.
    private static let pinViewport = """
    (function () {
      var m = document.querySelector('meta[name=viewport]');
      if (!m) { m = document.createElement('meta'); m.name = 'viewport'; document.head.appendChild(m); }
      m.content = 'width=device-width, initial-scale=1, minimum-scale=1, maximum-scale=1, user-scalable=no, viewport-fit=cover, interactive-widget=resizes-content';
    })();
    """

    init(url: URL) {
        origin = WebOrigin(url)
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .nonPersistent()
        view = WKWebView(frame: .zero, configuration: config)
        super.init()
        // The content controller keeps its handlers alive, so it gets a go-between
        // that holds this host weakly.
        config.userContentController.add(MessageProxy(host: self), name: "mad")
        view.navigationDelegate = self
        view.uiDelegate = self
        view.isOpaque = false
        // The page's own background (its --bg, light and dark), so a bounce at the
        // top or the bottom shows no white strip.
        let page = UIColor { $0.userInterfaceStyle == .dark ? UIColor(red: 0x13 / 255, green: 0x13 / 255, blue: 0x15 / 255, alpha: 1) : .white }
        view.backgroundColor = page
        view.scrollView.backgroundColor = page
        view.underPageBackgroundColor = page
        // An app, not a web page: no link previews, and (below, in the viewport) no zoom.
        view.allowsLinkPreview = false
        // The viewport pins the scale too, as the page loads (it is the page's own
        // `embed` that does the same for what is on screen).
        config.userContentController.addUserScript(WKUserScript(source: Self.pinViewport, injectionTime: .atDocumentEnd, forMainFrameOnly: true))
        // The page is edge to edge: it keeps clear of the status bar and the home
        // indicator itself, with env(safe-area-inset-*).
        view.scrollView.contentInsetAdjustmentBehavior = .never
        view.load(URLRequest(url: url))
    }

    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let url = action.request.url else { return decisionHandler(.cancel) }
        if WebOrigin(url) == origin {
            decisionHandler(.allow)
        } else {
            decisionHandler(.cancel)
            openOutside(url)
        }
    }

    // window.open and target=_blank: never a new web view; outside origins
    // go to the system browser.
    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if let url = action.request.url {
            if WebOrigin(url) == origin { webView.load(action.request) } else { openOutside(url) }
        }
        return nil
    }

    #if DEBUG
    /// Debug builds only: posts the message the page's menu posts, from the page
    /// itself, to see the bridge work end to end (`-debugSwitchMac YES`).
    func debugPostSwitchMac() {
        view.evaluateJavaScript("window.webkit.messageHandlers.mad.postMessage({type: 'switchMac'})")
    }
    #endif

    private func openOutside(_ url: URL) {
        guard let s = url.scheme?.lowercased(), s == "https" || s == "http" else { return }
        UIApplication.shared.open(url)
    }
}

/// Passes the page's messages to the host: the main frame of the paired Mac's
/// origin only, and only the types the app knows.
@MainActor
private final class MessageProxy: NSObject, WKScriptMessageHandler {
    weak var host: DeckWebHost?

    init(host: DeckWebHost) { self.host = host }

    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
        guard let host, message.frameInfo.isMainFrame else { return }
        let o = message.frameInfo.securityOrigin
        guard WebOrigin(scheme: o.protocol, host: o.host, port: o.port) == host.origin,
              let m = DeckMessage.parse(message.body) else { return }
        host.onMessage(m)
    }
}
