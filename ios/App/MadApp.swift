import MadKit
import SwiftUI

@main
struct MadApp: App {
    @StateObject private var store = MacStore()

    var body: some Scene {
        WindowGroup {
            MacsView(opening: MadApp.fixtureMac ?? store.lastMac).environmentObject(store)
        }
    }

    /// Debug builds only: a server to talk to instead of a paired Mac, for building and testing
    /// views against the fixture server (`MAD_FIXTURE_URL`, or `-fixtureURL <url>`). It is opened
    /// at once, and nothing of it is stored.
    private static var fixtureMac: Mac? {
        #if DEBUG
        FixtureHook.endpoint()?.mac(named: "Fixture")
        #else
        nil
        #endif
    }
}
