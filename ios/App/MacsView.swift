import MadKit
import SwiftUI

struct MacsView: View {
    @EnvironmentObject var store: MacStore
    @State private var scanning = false
    @State private var pasting = false
    @State private var pasted = ""
    @State private var renaming: Mac?
    @State private var newName = ""
    @State private var problem: String?
    @State private var path: [Mac]
    @State private var retrying: Set<UUID> = []
    @Environment(\.scenePhase) private var scenePhase
    /// Looked at again whenever the app appears or comes back, so a build left
    /// running that crosses the 2-day line shows its note.
    @State private var expiry: String?

    /// `opening` is the Mac last used: the app starts inside it, and on the list
    /// when there is none.
    init(opening: Mac? = nil) {
        _path = State(initialValue: opening.map { [$0] } ?? [])
    }

    var body: some View {
        NavigationStack(path: $path) {
            List {
                ForEach(store.pending) { p in
                    HStack {
                        VStack(alignment: .leading) {
                            Text(p.title).font(.body)
                            Text("Pairing not finished").font(.subheadline).foregroundStyle(.orange)
                        }
                        Spacer()
                        if retrying.contains(p.id) {
                            ProgressView()
                        } else {
                            Button("Retry") { Task { await finish(p) } }.buttonStyle(.bordered)
                        }
                    }
                }
                .onDelete { store.dropPending(at: $0) }
                ForEach(store.macs) { mac in
                    NavigationLink(value: mac) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(mac.name).font(.body)
                            Text(mac.baseURL.host ?? "").font(.subheadline).foregroundStyle(.secondary)
                        }
                    }
                    .contextMenu {
                        Button("Rename") { newName = mac.name; renaming = mac }
                    }
                }
                .onDelete { store.delete(at: $0) }
            }
            .overlay {
                if store.macs.isEmpty && store.pending.isEmpty {
                    ContentUnavailableView("No Mac yet", systemImage: "qrcode.viewfinder",
                        description: Text("Run mad phone on your Mac, then scan its QR code."))
                }
            }
            .navigationTitle("mad")
            .safeAreaInset(edge: .top, spacing: 0) {
                if let expiry {
                    Label(expiry, systemImage: "clock.badge.exclamationmark")
                        .font(.subheadline)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 16).padding(.vertical, 10)
                        .background(Color.orange.opacity(0.15))
                        .foregroundStyle(.orange)
                }
            }
            .onAppear {
                expiry = MacsView.expiryNotice()
                launchHooks()
            }
            .onChange(of: scenePhase) { _, phase in
                if phase == .active { expiry = MacsView.expiryNotice() }
            }
            .navigationDestination(for: Mac.self) { MacView(mac: $0) }
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        Button("Scan QR code", systemImage: "qrcode.viewfinder") { scanning = true }
                        Button("Paste link", systemImage: "doc.on.clipboard") { pasted = ""; pasting = true }
                    } label: { Image(systemName: "plus") }
                }
            }
            .fullScreenCover(isPresented: $scanning) {
                ScannerScreen { text in
                    scanning = false
                    if let text { pair(text) }
                }
            }
            .alert("Paste the link", isPresented: $pasting) {
                TextField("https://…/#token=…", text: $pasted)
                    .textInputAutocapitalization(.never).autocorrectionDisabled()
                Button("Pair") { pair(pasted) }
                Button("Cancel", role: .cancel) {}
            }
            .alert("Rename", isPresented: Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } })) {
                TextField("Name", text: $newName)
                Button("Save") { if let m = renaming { store.rename(m, to: newName) } }
                Button("Cancel", role: .cancel) {}
            }
            .alert("Cannot pair", isPresented: Binding(get: { problem != nil }, set: { if !$0 { problem = nil } })) {
                Button("OK", role: .cancel) {}
            } message: { Text(problem ?? "") }
        }
    }

    /// Debug builds only: pair with a local `mad desktop -serve` over http.
    private static var allowLocalHTTP: Bool {
        #if DEBUG
        true
        #else
        false
        #endif
    }

    /// The quiet notice for a build about to run out; nil while there is time.
    static func expiryNotice(now: Date = Date()) -> String? {
        ProfileExpiry.current(now: now)?.english
    }

    /// Debug builds only, for driving the app from the command line:
    /// `-pairLink <url>` pairs, `-openMac YES` then opens the first Mac.
    private func launchHooks() {
        #if DEBUG
        let d = UserDefaults.standard
        Task {
            if let text = d.string(forKey: "pairLink"), store.macs.isEmpty,
               let link = try? Pairing.parse(text, allowLocalHTTP: true) {
                do { try await store.add(link) } catch { report(error) }
            }
            if d.bool(forKey: "openMac"), path.isEmpty, let m = store.macs.first { path = [m] }
        }
        #endif
    }

    private func pair(_ text: String) {
        do {
            let link = try Pairing.parse(text, allowLocalHTTP: Self.allowLocalHTTP)
            Task {
                do { try await store.add(link) } catch { report(error) }
            }
        } catch { problem = error.localizedDescription }
    }

    private func finish(_ p: PairingBook.Pending) async {
        retrying.insert(p.id)
        defer { retrying.remove(p.id) }
        do { try await store.complete(p) } catch { report(error) }
    }

    /// A refused pairing says to scan a new QR; a lost connection says the
    /// pairing is kept and can be retried.
    private func report(_ error: Error) {
        if let e = error as? ConnectionError, e == .offline {
            problem = "The Mac could not be reached. The pairing is kept: tap Retry when it is back."
        } else {
            problem = error.localizedDescription
        }
    }
}
