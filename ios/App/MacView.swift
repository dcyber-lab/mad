import MadKit
import MadShell
import SwiftUI

/// An opened Mac: the deck page, full screen. The page draws its own header
/// (the Mac's name, the menu); its "switch Mac" asks the app to go back to the
/// Macs list.
struct MacView: View {
    @StateObject private var model: MacModel
    @EnvironmentObject private var store: MacStore
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase
    /// The build's standing when it is about to run out, looked at again when
    /// the app comes back; nil once put away for the day.
    @State private var expiry: ProvisioningProfile.Expiry?
    private static let dismissedKey = "expiryDismissedDay"

    init(mac: Mac) { _model = StateObject(wrappedValue: MacModel(mac: mac)) }

    /// Debug builds only: `-nativeHome YES` opens the native shell (MadUI) instead of the page.
    private static var nativeHome: Bool {
        #if DEBUG
        UserDefaults.standard.bool(forKey: "nativeHome")
        #else
        false
        #endif
    }

    var body: some View {
        if Self.nativeHome {
            NativeRoot(api: MadClient(mac: model.mac), title: model.mac.name, onSwitchMac: { dismiss() })
        } else {
            pageBody
        }
    }

    private var pageBody: some View {
        VStack(spacing: 0) {
            if let expiry {
                ExpiryBar(text: expiry.chinese) {
                    UserDefaults.standard.set(ExpiryDismissal.dayKey(Date()), forKey: Self.dismissedKey)
                    self.expiry = nil
                }
            }
            // Edge to edge; under a banner the page starts below it, which already clears the status bar.
            DeckWebView(host: model.web)
                .ignoresSafeArea(.container, edges: expiry == nil ? .all : [.horizontal, .bottom])
        }
        .background(Color(.systemBackground).ignoresSafeArea())
            .onChange(of: scenePhase) { _, phase in if phase == .active { refreshExpiry() } }
            .toolbar(.hidden, for: .navigationBar)
            .navigationBarBackButtonHidden(true)
            .onAppear {
                model.web.onMessage = { message in
                    switch message {
                    case .switchMac: dismiss()
                    }
                }
                store.opened(model.mac)
                refreshExpiry()
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

    private func refreshExpiry() {
        let day = UserDefaults.standard.string(forKey: Self.dismissedKey)
        expiry = ExpiryDismissal.isDismissed(storedDay: day, now: Date()) ? nil : ProfileExpiry.current()
    }
}
