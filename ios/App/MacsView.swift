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
    @State private var hooked = false
    @Environment(\.scenePhase) private var scenePhase
    /// Looked at again whenever the app appears or comes back, so a build left
    /// running that crosses the 2-day line shows its note.
    @State private var expiry: String?

    /// What the person chose this session, so the menu's wording follows at once.
    @State private var webChoice: [UUID: Bool] = [:]

    /// `opening` is the Mac last used: the app starts inside it, and on the list
    /// when there is none.
    init(opening: Mac? = nil) {
        _path = State(initialValue: opening.map { [$0] } ?? [])
    }

    var body: some View {
        NavigationStack {
            List {
                ForEach(store.pending) { p in
                    HStack {
                        VStack(alignment: .leading) {
                            Text(p.title).font(.body)
                            Text("还没配对完").font(.subheadline).foregroundStyle(.orange)
                        }
                        Spacer()
                        if retrying.contains(p.id) {
                            ProgressView()
                        } else {
                            Button("重试") { Task { await finish(p) } }.buttonStyle(.bordered)
                        }
                    }
                }
                .onDelete { store.dropPending(at: $0) }
                ForEach(store.macs) { mac in
                    // The Mac opens over the list (not pushed on it): its screens have navigation of their own.
                    Button { path = [mac] } label: {
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(mac.name).font(.body).foregroundStyle(.primary)
                                Text(mac.baseURL.host ?? "").font(.subheadline).foregroundStyle(.secondary)
                            }
                            Spacer()
                            Image(systemName: "chevron.right").font(.footnote.weight(.semibold)).foregroundStyle(.tertiary)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .contextMenu {
                        Button("改名") { newName = mac.name; renaming = mac }
                        Button((webChoice[mac.id] ?? WebDeckChoice.uses(for: mac)) ? "使用原生版" : "使用网页版") {
                            let on = !(webChoice[mac.id] ?? WebDeckChoice.uses(for: mac))
                            WebDeckChoice.set(on, for: mac)
                            webChoice[mac.id] = on
                        }
                    }
                }
                .onDelete { store.delete(at: $0) }
            }
            .overlay {
                if store.macs.isEmpty && store.pending.isEmpty {
                    ContentUnavailableView("还没有 Mac", systemImage: "qrcode.viewfinder",
                        description: Text("在 Mac 上运行 mad phone，\n然后扫描它的二维码。"))
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
                // The Debug hooks run once: coming back to the list must not open the Mac again.
                if !hooked { hooked = true; launchHooks() }
            }
            .onChange(of: scenePhase) { _, phase in
                if phase == .active { expiry = MacsView.expiryNotice() }
            }
            .fullScreenCover(isPresented: Binding(get: { !path.isEmpty }, set: { if !$0 { path = [] } })) {
                if let mac = path.first { MacView(mac: mac) }
            }
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        Button("扫描二维码", systemImage: "qrcode.viewfinder") { scanning = true }
                        Button("粘贴链接", systemImage: "doc.on.clipboard") { pasted = ""; pasting = true }
                    } label: { Image(systemName: "plus") }
                }
            }
            .fullScreenCover(isPresented: $scanning) {
                ScannerScreen { text in
                    scanning = false
                    if let text { pair(text) }
                }
            }
            .alert("粘贴链接", isPresented: $pasting) {
                TextField("https://…/#token=…", text: $pasted)
                    .textInputAutocapitalization(.never).autocorrectionDisabled()
                Button("配对") { pair(pasted) }
                Button("取消", role: .cancel) {}
            }
            .alert("改名", isPresented: Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } })) {
                TextField("名字", text: $newName)
                Button("保存") { if let m = renaming { store.rename(m, to: newName) } }
                Button("取消", role: .cancel) {}
            }
            .alert("配对失败", isPresented: Binding(get: { problem != nil }, set: { if !$0 { problem = nil } })) {
                Button("好", role: .cancel) {}
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
