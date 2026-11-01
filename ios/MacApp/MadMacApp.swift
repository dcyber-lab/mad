import AppKit
import MadKit
import MadShell
import SwiftUI
import UniformTypeIdentifiers
import WebKit

/// mad for the Mac. It talks to the server of the Mac it runs on: the phone server if that is up,
/// else a `mad desktop -no-window` of its own (see `ServerModel`).
@main
struct MadMacApp: App {
    @State private var server = ServerModel()

    var body: some Scene {
        WindowGroup {
            RootView(server: server)
                .frame(minWidth: 800, minHeight: 520)
        }
        .defaultSize(width: 1200, height: 800)
        // The page, in a window of its own, for what has no native screen yet (usage, the board, an
        // agent's terminal ...). The server is the one the main window found.
        WindowGroup("mad", for: WebTarget.self) { $target in
            if let target, case .ready(let endpoint) = server.state {
                WebPage(url: endpoint.mac().webURL(target, embedded: false))
                    .frame(minWidth: 720, minHeight: 480)
            } else {
                Text("mad is not running").padding(32)
            }
        }
        Settings { SettingsView() }
    }
}

/// The page in a WKWebView. Only the server's own origin loads in it; other links go to the browser.
struct WebPage: NSViewRepresentable {
    let url: URL

    func makeCoordinator() -> Coordinator { Coordinator(origin: WebOrigin(url)) }

    func makeNSView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .nonPersistent()
        let view = WKWebView(frame: .zero, configuration: config)
        view.navigationDelegate = context.coordinator
        view.uiDelegate = context.coordinator
        view.load(URLRequest(url: url))
        return view
    }

    func updateNSView(_ view: WKWebView, context: Context) {}

    final class Coordinator: NSObject, WKNavigationDelegate, WKUIDelegate {
        let origin: WebOrigin
        init(origin: WebOrigin) { self.origin = origin }

        /// A file input (the page's 📎): the page asks the app for the open panel.
        func webView(_ webView: WKWebView, runOpenPanelWith parameters: WKOpenPanelParameters,
                     initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping ([URL]?) -> Void) {
            let panel = NSOpenPanel()
            panel.allowsMultipleSelection = parameters.allowsMultipleSelection
            panel.canChooseDirectories = parameters.allowsDirectories
            panel.allowedContentTypes = [.image]
            if let window = webView.window {
                panel.beginSheetModal(for: window) { completionHandler($0 == .OK ? panel.urls : nil) }
            } else {
                completionHandler(panel.runModal() == .OK ? panel.urls : nil)
            }
        }

        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                     decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            guard let url = action.request.url else { return decisionHandler(.cancel) }
            if WebOrigin(url) == origin { return decisionHandler(.allow) }
            decisionHandler(.cancel)
            if let s = url.scheme?.lowercased(), s == "https" || s == "http" { NSWorkspace.shared.open(url) }
        }
    }
}

/// Finds the server and keeps it: a server given by the fixture hook (`MAD_FIXTURE_URL` or
/// `-fixtureURL`), else the phone server on 127.0.0.1:7484 with the token in mad's state
/// directory, else one the app starts itself and stops when it quits.
@MainActor @Observable
final class ServerModel {
    enum State {
        case locating
        case ready(Endpoint)
        case failed(String)
    }

    private(set) var state: State = .locating
    @ObservationIgnored private let launcher = ServerLauncher()

    /// Where `mad` is, if the person set it in Settings; empty means the first one on PATH.
    static let madPathKey = "madPath"
    /// Whether the window shows the whole web page instead of the native screens.
    static let webDeckKey = "webDeck"

    func start() async {
        state = .locating
        if let fixture = FixtureHook.endpoint() {
            state = .ready(fixture)
            return
        }
        if let phone = await LocalServer.probePhoneServer() {
            state = .ready(phone)
            return
        }
        let chosen = UserDefaults.standard.string(forKey: Self.madPathKey)
        guard let mad = LocalServer.findMad(path: chosen) else {
            state = .failed(LocalServer.LaunchError.madNotFound.localizedDescription)
            return
        }
        do {
            state = .ready(try await launcher.start(binary: mad))
        } catch {
            state = .failed(error.localizedDescription)
        }
    }

    /// Stops the server the app started (the phone server is not the app's to stop).
    func stop() { launcher.stop() }
}

struct RootView: View {
    let server: ServerModel
    @Environment(\.openWindow) private var openWindow
    /// "Use the web version" in Settings: the whole page in this window, as before the native screens.
    @AppStorage(ServerModel.webDeckKey) private var webDeck = false

    var body: some View {
        Group {
            switch server.state {
            case .locating:
                ProgressView("Looking for mad…")
            case .failed(let why):
                VStack(spacing: 12) {
                    Text("mad is not running").font(.title2.weight(.semibold))
                    Text(why).foregroundStyle(.secondary).multilineTextAlignment(.center)
                    HStack {
                        Button("Try again") { Task { await server.start() } }
                        SettingsLink { Text("Settings…") }
                    }
                }
                .padding(32)
            case .ready(let endpoint):
                if webDeck {
                    WebPage(url: endpoint.mac().webURL(nil, embedded: false))
                } else {
                    NativeRoot(api: endpoint.client(), title: "mad", openWeb: { openWindow(value: $0) })
                }
            }
        }
        .task { await server.start() }
        #if DEBUG
        .task { await Self.snapshotIfAsked() }
        #endif
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.willTerminateNotification)) { _ in
            server.stop()
        }
    }
}

#if DEBUG
extension RootView {
    /// Debug builds only: `-snapshotTo <path>` writes the window's own view to a PNG once it has
    /// settled, then quits. The system's window capture needs a permission a build script lacks.
    @MainActor static func snapshotIfAsked() async {
        let args = ProcessInfo.processInfo.arguments
        guard let i = args.firstIndex(of: "-snapshotTo"), i + 1 < args.count else { return }
        try? await Task.sleep(for: .seconds(6))
        guard let view = NSApp.windows.first(where: { $0.contentView != nil })?.contentView?.superview,
              let rep = view.bitmapImageRepForCachingDisplay(in: view.bounds) else { return }
        view.cacheDisplay(in: view.bounds, to: rep)
        try? rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: args[i + 1]))
        NSApp.terminate(nil)
    }
}
#endif

struct SettingsView: View {
    @AppStorage(ServerModel.madPathKey) private var madPath = ""
    @AppStorage(ServerModel.webDeckKey) private var webDeck = false

    var body: some View {
        Form {
            Toggle("使用网页版", isOn: $webDeck)
            Text("Show the whole web page in the window instead of the native screens, if something is wrong with them.")
                .font(.footnote).foregroundStyle(.secondary)
            TextField("Path to mad", text: $madPath, prompt: Text("the first mad on your PATH"))
            Text("The app uses the phone server when it is on, and starts mad itself when it is not.")
                .font(.footnote).foregroundStyle(.secondary)
        }
        .padding(20)
        .frame(width: 460)
    }
}
