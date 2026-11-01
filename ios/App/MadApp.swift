import MadKit
import MadShell
import SwiftUI

@main
struct MadApp: App {
    @StateObject private var store = MacStore()

    var body: some Scene {
        WindowGroup {
            if let mac = MadApp.nativeRootMac {
                NativeRoot(api: MadClient(mac: mac), title: mac.name)
            } else {
                MacsView(opening: MadApp.fixtureMac ?? store.lastMac).environmentObject(store)
            }
        }
    }

    /// Debug builds only: a server to talk to instead of a paired Mac, for building and testing
    /// views against the fixture server (`MAD_FIXTURE_URL`, or `-fixtureURL <url>`). It is opened
    /// at once, and nothing of it is stored.
    /// Debug builds only: `-nativeRoot YES` with a fixture server (`-fixtureURL`) opens the native shell
    /// at once, without the list of Macs, to look at its views.
    private static var nativeRootMac: Mac? {
        #if DEBUG
        UserDefaults.standard.bool(forKey: "nativeRoot") ? FixtureHook.endpoint()?.mac(named: "Fixture") : nil
        #else
        nil
        #endif
    }

    private static var fixtureMac: Mac? {
        #if DEBUG
        FixtureHook.endpoint()?.mac(named: "Fixture")
        #else
        nil
        #endif
    }
}
