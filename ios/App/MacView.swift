import MadKit
import SwiftUI

/// An opened Mac: the deck page, full screen. The page draws its own header
/// (the Mac's name, the menu); its "switch Mac" asks the app to go back to the
/// Macs list.
struct MacView: View {
    @StateObject private var model: MacModel
    @EnvironmentObject private var store: MacStore
    @Environment(\.dismiss) private var dismiss

    init(mac: Mac) { _model = StateObject(wrappedValue: MacModel(mac: mac)) }

    var body: some View {
        DeckWebView(host: model.web)
            .ignoresSafeArea(.container)
            .background(Color(.systemBackground).ignoresSafeArea())
            .toolbar(.hidden, for: .navigationBar)
            .navigationBarBackButtonHidden(true)
            .onAppear {
                model.web.onMessage = { message in
                    switch message {
                    case .switchMac: dismiss()
                    }
                }
                store.opened(model.mac)
            }
            .task {
                #if DEBUG
                if UserDefaults.standard.bool(forKey: "debugSwitchMac") {
                    try? await Task.sleep(for: .seconds(6))
                    model.web.debugPostSwitchMac()
                }
                #endif
                if let name = await model.machineName() { store.adoptMachineName(name, for: model.mac) }
            }
    }
}
