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

    /// The page, shown over the native screens for what has no native screen yet.
    @State private var web: WebSheet?

    /// The native screens are the app; the person can ask for the whole web page of a Mac instead
    /// (`WebDeckChoice`), and `-webDeck YES` / `-webDeck NO` in Debug builds forces either.
    private var usesWebDeck: Bool { WebDeckChoice.uses(for: model.mac) }

    var body: some View {
        if usesWebDeck {
            pageBody
        } else {
            NativeRoot(api: MadClient(mac: model.mac), title: model.mac.name, onSwitchMac: { dismiss() },
                       openWeb: { web = WebSheet(mac: model.mac, target: $0) })
                .onAppear { store.opened(model.mac); refreshExpiry() }
                .task { if let name = await model.machineName() { store.adoptMachineName(name, for: model.mac) } }
                .sheet(item: $web) { WebSheetView(sheet: $0) }
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

/// Which Macs show the whole web page instead of the native screens: a person's choice per Mac, kept
/// on this device ("使用网页版" in the Macs list).
enum WebDeckChoice {
    private static func key(_ mac: Mac) -> String { "webDeck." + mac.id.uuidString }

    static func uses(for mac: Mac) -> Bool {
        #if DEBUG
        if let forced = UserDefaults.standard.object(forKey: "webDeck") as? String { return forced == "YES" }
        #endif
        return UserDefaults.standard.bool(forKey: key(mac))
    }

    static func set(_ on: Bool, for mac: Mac) {
        if on { UserDefaults.standard.set(true, forKey: key(mac)) } else { UserDefaults.standard.removeObject(forKey: key(mac)) }
    }
}

/// The page on one view, in a sheet.
struct WebSheet: Identifiable {
    let mac: Mac
    let target: WebTarget
    var id: String { mac.id.uuidString + target.id }
}

struct WebSheetView: View {
    let sheet: WebSheet
    @State private var host: DeckWebHost
    @Environment(\.dismiss) private var dismiss

    init(sheet: WebSheet) {
        self.sheet = sheet
        _host = State(initialValue: DeckWebHost(url: sheet.mac.webURL(sheet.target)))
    }

    var body: some View {
        NavigationStack {
            DeckWebView(host: host)
                .ignoresSafeArea(.container, edges: .bottom)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() } } }
        }
    }
}
