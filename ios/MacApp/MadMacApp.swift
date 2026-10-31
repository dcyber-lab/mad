import AppKit
import MadKit
import MadUI
import SwiftUI

/// mad for the Mac. It talks to the server of the Mac it runs on: the phone server if that is up,
/// else a `mad desktop -no-window` of its own (see `ServerModel`).
@main
struct MadMacApp: App {
    @State private var server = ServerModel()

    var body: some Scene {
        WindowGroup {
            RootView(server: server)
                .frame(minWidth: 720, minHeight: 480)
        }
        Settings { SettingsView() }
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
                NativeShell(api: endpoint.client(), title: "mad")
            }
        }
        .task { await server.start() }
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.willTerminateNotification)) { _ in
            server.stop()
        }
    }
}

struct SettingsView: View {
    @AppStorage(ServerModel.madPathKey) private var madPath = ""

    var body: some View {
        Form {
            TextField("Path to mad", text: $madPath, prompt: Text("the first mad on your PATH"))
            Text("The app uses the phone server when it is on, and starts mad itself when it is not.")
                .font(.footnote).foregroundStyle(.secondary)
        }
        .padding(20)
        .frame(width: 460)
    }
}
