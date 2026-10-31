import MadKit
import SwiftUI
import UIKit
import WebKit

/// One open Mac: its latest snapshot, how the connection stands, acting on
/// inbox items, and the deck page's web view, created once so switching tabs
/// never reloads it.
@MainActor
final class MacModel: ObservableObject {
    enum Link: Equatable {
        case connecting
        case ok
        case failed(ConnectionError)
    }

    let mac: Mac
    @Published private(set) var snapshot: DeckSnapshot?
    @Published private(set) var link: Link = .connecting
    @Published private(set) var busy: Set<String> = []
    /// A server message to show the person (a 409's text, a failed act).
    @Published var notice: String?

    /// The Mac's own name, from each snapshot: the store adopts it unless the
    /// person renamed the Mac.
    var onMachineName: (String?) -> Void = { _ in }

    let client: MadClient
    private(set) lazy var web: DeckWebHost = DeckWebHost(url: mac.embeddedDeckURL)
    /// Debug builds only: a sample snapshot instead of a live Mac; acts send nothing.
    private let fixture: Bool

    init(mac: Mac) {
        self.mac = mac
        self.client = MadClient(mac: mac)
        #if DEBUG
        let sample = Fixture.snapshot()
        fixture = sample != nil
        if let sample {
            snapshot = sample
            link = .ok
        }
        #else
        fixture = false
        #endif
    }

    var needs: Int { snapshot?.needs ?? 0 }

    /// Debug builds only: a sample conversation to read without a live Mac.
    func sampleChat(for agentID: String) -> (page: ChatPage, live: LiveMessage?)? {
        fixture ? Fixture.chat(for: agentID) : nil
    }

    func refresh() async {
        if fixture { return }
        do {
            snapshot = try await client.deck()
            link = .ok
            onMachineName(snapshot?.machine)
        } catch let e as ConnectionError {
            link = .failed(e)
        } catch {
            link = .failed(.offline)
        }
    }

    /// Polls while the calling task lives; cancel it to stop.
    func poll(every seconds: Double = 3) async {
        while !Task.isCancelled {
            await refresh()
            try? await Task.sleep(for: .seconds(seconds))
        }
    }

    func perform(_ item: DeckSnapshot.InboxItem, _ act: DeckSnapshot.Act, text: String? = nil) async {
        // A second tap on the same item while one is in flight does nothing.
        guard busy.insert(item.key).inserted else { return }
        defer { busy.remove(item.key) }
        if fixture {
            notice = "Sample data: nothing was sent."
            return
        }
        do {
            try await client.act(key: item.key, verb: act.verb, option: act.option, text: text)
            UINotificationFeedbackGenerator().notificationOccurred(.success)
        } catch let e as ConnectionError {
            UINotificationFeedbackGenerator().notificationOccurred(.error)
            notice = e.localizedDescription
        } catch {
            notice = error.localizedDescription
        }
        await refresh()
    }
}

/// Owns the deck page's WKWebView. Only the Mac's own origin loads in it;
/// the data store is not persistent, so the page's copy of the token dies
/// with the view and the Keychain stays the only durable one.
final class DeckWebHost: NSObject, WKNavigationDelegate, WKUIDelegate {
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

    let view: WKWebView
    private let origin: Origin

    init(url: URL) {
        origin = Origin(url)
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .nonPersistent()
        view = WKWebView(frame: .zero, configuration: config)
        super.init()
        view.navigationDelegate = self
        view.uiDelegate = self
        view.isOpaque = false
        view.backgroundColor = .systemBackground
        view.load(URLRequest(url: url))
    }

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

    // window.open and target=_blank: never a new web view; outside origins
    // go to the system browser.
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
