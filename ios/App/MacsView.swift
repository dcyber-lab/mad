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

    var body: some View {
        NavigationStack {
            List {
                ForEach(store.macs) { mac in
                    NavigationLink(value: mac) {
                        VStack(alignment: .leading) {
                            Text(mac.name).font(.headline)
                            Text(mac.baseURL.host ?? "").font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    .contextMenu {
                        Button("Rename") { newName = mac.name; renaming = mac }
                    }
                }
                .onDelete { store.delete(at: $0) }
            }
            .overlay {
                if store.macs.isEmpty {
                    ContentUnavailableView("No Mac yet", systemImage: "qrcode.viewfinder",
                        description: Text("Run `mad phone` on your Mac, then scan its QR code."))
                }
            }
            .navigationTitle("mad")
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

    private func pair(_ text: String) {
        do { store.add(try Pairing.parse(text, allowLocalHTTP: Self.allowLocalHTTP)) }
        catch { problem = error.localizedDescription }
    }
}
