#if os(macOS)
import AppKit
import MadKit
import MadUI
import SwiftUI

/// 在你的手机上: the Mac window put on the phone. Off, it says how and offers to turn it on; on, it shows the QR code
/// of the address, the address, and the phones paired, each with a way to remove it.
struct PhoneSheet: View {
    let api: PhoneAPI
    let close: () -> Void

    @State private var status: PhoneStatus?
    @State private var devices: [PhoneDevice] = []
    @State private var qr: NSImage?
    @State private var failure: String?
    @State private var busy = false
    @State private var copied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(L("在你的手机上")).font(.system(size: 20, weight: .semibold))
            content
            if let failure { Text(failure).font(Font.system(size: 13)).foregroundStyle(.orange).fixedSize(horizontal: false, vertical: true) }
            HStack {
                if status?.on == true {
                    Button(L("关闭入口")) { Task { await set(on: false) } }.disabled(busy)
                }
                Spacer()
                if status?.on == false && status?.problem == nil {
                    Button(L("取消"), action: close)
                    Button(L("打开")) { Task { await set(on: true) } }.keyboardShortcut(.defaultAction).disabled(busy)
                } else {
                    Button(L("完成"), action: close).keyboardShortcut(.defaultAction)
                }
            }
        }
        .padding(24)
        .frame(width: 460)
        .background(Color.chatComposer)
        // "4 天前" in Chinese, "4 days ago" in English: the relative times follow the words
        .environment(\.locale, Locale(identifier: MadLocale.isEnglish(bundle: .module) ? "en_US" : "zh_CN"))
        .task { await load() }
    }

    @ViewBuilder private var content: some View {
        if let status {
            if let problem = status.problem {
                Text(problem).font(Font.system(size: 15)).fixedSize(horizontal: false, vertical: true)
            } else if !status.on {
                Text(status.via == "cloudflare"
                     ? L("通过 Cloudflare Tunnel 把这个窗口送到你的手机上，在哪里都能用：知道地址的人都能连上，但进去要输入它显示的验证码。")
                     : status.host.map { Lf("通过 Tailscale 把这个窗口送到你的手机上，地址是 %@：只有你自己的设备连得上，进去要输入它显示的验证码。", $0) }
                        ?? L("通过 Tailscale 把这个窗口送到你的手机上：只有你自己的设备连得上，进去要输入它显示的验证码。"))
                    .font(Font.system(size: 15)).fixedSize(horizontal: false, vertical: true)
            } else {
                on(status)
            }
        } else if failure == nil {
            ProgressView().frame(maxWidth: .infinity, minHeight: 120)
        }
    }

    private func on(_ status: PhoneStatus) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            Group {
                if let qr { Image(nsImage: qr).interpolation(.none).resizable().scaledToFit() } else { Color.clear }
            }
            .frame(width: 220, height: 220).padding(10).background(.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .frame(maxWidth: .infinity)
            Text(status.via == "cloudflare"
                 ? L("用手机相机扫它：里面的验证码只能用一次，10 分钟内有效，手机会得到一把自己的钥匙。在那里“添加到主屏幕”，下次就方便了。")
                 : L("用手机相机扫它（手机要在你的 tailnet 里）：里面的验证码只能用一次，10 分钟内有效，手机会得到一把自己的钥匙。在那里“添加到主屏幕”，下次就方便了。"))
                .font(Font.system(size: 13)).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            HStack(spacing: 8) {
                Text(status.url ?? "").font(Font.system(size: 12, design: .monospaced)).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
                Spacer(minLength: 0)
                Button(copied ? L("已复制") : L("复制")) {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(status.url ?? "", forType: .string)
                    copied = true
                }
                .buttonStyle(.borderless)
            }
            .padding(.horizontal, 12).padding(.vertical, 8)
            .background(Color.primary.opacity(0.06), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
            if !devices.isEmpty {
                Text(L("已配对的手机")).font(Font.system(size: 13)).foregroundStyle(.secondary)
                ForEach(devices) { d in
                    HStack {
                        Text(d.name).font(Font.system(size: 15))
                        if let seen = d.lastSeen {
                            Text(seen, format: .relative(presentation: .named)).font(Font.system(size: 13)).foregroundStyle(.secondary)
                        }
                        Spacer(minLength: 0)
                        Button(L("移除")) { Task { await remove(d) } }.buttonStyle(.borderless).foregroundStyle(Color.madDanger)
                    }
                }
            }
        }
    }

    private func load() async {
        do {
            let s = try await api.phoneStatus()
            status = s
            if s.on { await loadOn() }
        } catch { failure = message(error) }
    }

    private func loadOn() async {
        async let q = api.phoneQR()
        async let d = api.phoneDevices()
        qr = (try? await q).flatMap { NSImage(data: $0) }
        devices = (try? await d) ?? []
    }

    private func set(on: Bool) async {
        busy = true
        failure = nil
        defer { busy = false }
        do {
            let s = try await api.phoneSet(on: on)
            status = s
            if s.on { await loadOn() } else { qr = nil; devices = [] }
        } catch { failure = message(error) }
    }

    private func remove(_ d: PhoneDevice) async {
        do {
            try await api.phoneRemove(id: d.id)
            devices.removeAll { $0.id == d.id }
        } catch { failure = message(error) }
    }

    private func message(_ error: Error) -> String { (error as? LocalizedError)?.errorDescription ?? error.localizedDescription }
}
#endif
