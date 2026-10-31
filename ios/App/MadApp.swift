import SwiftUI

@main
struct MadApp: App {
    @StateObject private var store = MacStore()

    var body: some Scene {
        WindowGroup {
            MacsView().environmentObject(store)
        }
    }
}
