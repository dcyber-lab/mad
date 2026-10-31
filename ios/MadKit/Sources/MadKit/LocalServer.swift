import Foundation

/// Where a server is and the token that opens it.
public struct Endpoint: Equatable, Sendable {
    public var base: URL
    public var token: String
    public init(base: URL, token: String) { self.base = base; self.token = token }

    /// This server as a Mac the client can talk to.
    public func mac(named name: String = "This Mac") -> Mac { Mac(name: name, baseURL: base, token: token) }
    /// A client for it.
    public func client(session: URLSession = .shared) -> MadClient { MadClient(mac: mac(), session: session) }
}

/// Finding the server of the Mac the app runs on: the phone server if it is up, else one of its own.
public enum LocalServer {
    /// The port `mad phone on` serves on (internal/phone).
    public static let phonePort = 7484

    /// mad's state directory: `$XDG_STATE_HOME/mad`, or `~/.local/state/mad`.
    public static func stateDirectory(environment: [String: String] = ProcessInfo.processInfo.environment,
                                      home: URL = URL(fileURLWithPath: NSHomeDirectory())) -> URL {
        if let x = environment["XDG_STATE_HOME"], !x.isEmpty { return URL(fileURLWithPath: x).appendingPathComponent("mad") }
        return home.appendingPathComponent(".local/state/mad")
    }

    /// The phone server's master token, from `phone_token` in the state directory; nil when there is none.
    public static func phoneToken(stateDirectory: URL) -> String? {
        guard let raw = try? String(contentsOf: stateDirectory.appendingPathComponent("phone_token"), encoding: .utf8) else { return nil }
        let t = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        return t.isEmpty ? nil : t
    }

    /// Reads the line `mad desktop -no-window` prints: `http://127.0.0.1:<port>/#token=<token>`.
    public static func parseReadyLine(_ line: String) -> Endpoint? {
        let s = line.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let c = URLComponents(string: s), let scheme = c.scheme, scheme == "http" || scheme == "https",
              let host = c.host, let frag = c.fragment, frag.hasPrefix("token="), frag.count > "token=".count else { return nil }
        var base = URLComponents()
        base.scheme = scheme
        base.host = host
        base.port = c.port
        guard let url = base.url else { return nil }
        return Endpoint(base: url, token: String(frag.dropFirst("token=".count)))
    }

    /// The `mad` to run: `path` if it is an executable file, else the first `mad` on PATH.
    public static func findMad(path: String? = nil, environment: [String: String] = ProcessInfo.processInfo.environment) -> URL? {
        let fm = FileManager.default
        if let p = path, !p.isEmpty {
            return fm.isExecutableFile(atPath: p) ? URL(fileURLWithPath: p) : nil
        }
        let search = (environment["PATH"] ?? "/usr/bin:/bin") + ":/opt/homebrew/bin:/usr/local/bin:" + NSHomeDirectory() + "/.local/bin"
        for dir in search.split(separator: ":") {
            let candidate = URL(fileURLWithPath: String(dir)).appendingPathComponent("mad")
            var isDir: ObjCBool = false
            if fm.fileExists(atPath: candidate.path, isDirectory: &isDir), !isDir.boolValue, fm.isExecutableFile(atPath: candidate.path) {
                return candidate
            }
        }
        return nil
    }

    /// The phone server on this Mac, if it is up and takes the token from the state directory.
    public static func probePhoneServer(stateDirectory: URL = stateDirectory(), port: Int = phonePort,
                                        session: URLSession = .shared) async -> Endpoint? {
        guard let token = phoneToken(stateDirectory: stateDirectory),
              let base = URL(string: "http://127.0.0.1:\(port)") else { return nil }
        let endpoint = Endpoint(base: base, token: token)
        var request = endpoint.client(session: session).request("/api/deck")
        request.timeoutInterval = 2
        guard let (_, response) = try? await session.data(for: request), (response as? HTTPURLResponse)?.statusCode == 200 else { return nil }
        return endpoint
    }

    public enum LaunchError: Error, Equatable, LocalizedError {
        case madNotFound
        case failed(String)
        case timeout

        public var errorDescription: String? {
            switch self {
            case .madNotFound: return "mad was not found. Install it, or set its path in Settings."
            case .failed(let why): return "mad could not start: \(why)"
            case .timeout: return "mad did not say where it listens."
            }
        }
    }
}

#if os(macOS)
/// A `mad desktop -no-window` of the app's own: started on a free port with a token of its own, and
/// stopped when the app quits.
public final class ServerLauncher: @unchecked Sendable {
    private var process: Process?
    /// The write end of the child's stdin. The app holds it, so when the app dies, however it dies,
    /// the kernel closes it and the child (`-exit-on-stdin-eof`) sees EOF and exits.
    private var lifeline: FileHandle?
    private let lock = NSLock()

    public init() {}

    deinit { stop() }

    /// Starts `binary` with `arguments` and waits for the line that says where it listens.
    public func start(binary: URL, arguments: [String] = ["desktop", "-no-window", "-addr", "127.0.0.1:0", "-exit-on-stdin-eof"],
                      timeout: TimeInterval = 20) async throws -> Endpoint {
        stop()
        let p = Process()
        p.executableURL = binary
        p.arguments = arguments
        // mad refuses to start a window inside its own deck: the app is not in one.
        var env = ProcessInfo.processInfo.environment
        env.removeValue(forKey: "TMUX")
        p.environment = env
        let out = Pipe()
        p.standardOutput = out
        p.standardError = FileHandle.nullDevice
        let life = Pipe()
        p.standardInput = life.fileHandleForReading
        do { try p.run() } catch { throw LocalServer.LaunchError.failed(error.localizedDescription) }
        // The child has its own copy of the read end; this one would keep the pipe open if it stayed.
        try? life.fileHandleForReading.close()
        remember(p, lifeline: life.fileHandleForWriting)

        // The first line it prints is the URL. Read as it comes, with a timer beside it: a read that
        // waits for output cannot be cancelled, so a silent server would hold it until it exits.
        let line: String? = await withCheckedContinuation { (continuation: CheckedContinuation<String?, Never>) in
            let once = Once(continuation)
            let buffer = LineBuffer()
            out.fileHandleForReading.readabilityHandler = { handle in
                let data = handle.availableData
                if data.isEmpty { once.finish(nil); return } // it closed its output
                if let line = buffer.add(data) { once.finish(line) }
            }
            DispatchQueue.global().asyncAfter(deadline: .now() + timeout) { once.finish(nil) }
        }
        out.fileHandleForReading.readabilityHandler = nil
        guard let line else {
            let running = p.isRunning
            stop()
            throw running ? LocalServer.LaunchError.timeout : LocalServer.LaunchError.failed("it exited at once")
        }
        guard let endpoint = LocalServer.parseReadyLine(line) else {
            stop()
            throw LocalServer.LaunchError.failed("unexpected output: \(line.prefix(80))")
        }
        return endpoint
    }

    /// Resumes a continuation once, whichever of the line, the end of output and the timer comes first.
    private final class Once: @unchecked Sendable {
        private let lock = NSLock()
        private var continuation: CheckedContinuation<String?, Never>?
        init(_ c: CheckedContinuation<String?, Never>) { continuation = c }
        func finish(_ line: String?) {
            lock.lock()
            let c = continuation
            continuation = nil
            lock.unlock()
            c?.resume(returning: line)
        }
    }

    /// Collects output until a newline.
    private final class LineBuffer: @unchecked Sendable {
        private let lock = NSLock()
        private var data = Data()
        func add(_ d: Data) -> String? {
            lock.lock(); defer { lock.unlock() }
            data.append(d)
            guard let nl = data.firstIndex(of: 10) else { return nil }
            return String(decoding: data[..<nl], as: UTF8.self)
        }
    }

    /// The server's process id while it runs.
    public var processIdentifier: Int32? {
        lock.lock(); defer { lock.unlock() }
        return process?.processIdentifier
    }

    private func remember(_ p: Process, lifeline l: FileHandle) {
        lock.lock()
        process = p
        lifeline = l
        lock.unlock()
    }

    public var isRunning: Bool {
        lock.lock(); defer { lock.unlock() }
        return process?.isRunning ?? false
    }

    /// Stops the server (idempotent).
    public func stop() {
        lock.lock()
        let p = process
        process = nil
        let l = lifeline
        lifeline = nil
        lock.unlock()
        try? l?.close()
        if let p, p.isRunning {
            p.terminate()
            // Not waitUntilExit: it can miss an exit that races the call and then waits forever.
            // Polling is bounded, and a server that ignores SIGTERM gets SIGKILL.
            var waited = 0.0
            while p.isRunning, waited < 3 {
                Thread.sleep(forTimeInterval: 0.02)
                waited += 0.02
            }
            if p.isRunning { kill(p.processIdentifier, SIGKILL) }
        }
    }
}
#endif

/// A server to talk to instead of the real one, for building and testing views against the fixture
/// server (`go run ./internal/desktop/fixture -scenario <name>`): the environment variable
/// `MAD_FIXTURE_URL`, or the launch argument `-fixtureURL <url>`.
public enum FixtureHook {
    /// The token the fixture server takes (it takes any, but it must look like one).
    public static let token = String(repeating: "0", count: 32)

    public static func endpoint(environment: [String: String] = ProcessInfo.processInfo.environment,
                                arguments: [String] = ProcessInfo.processInfo.arguments) -> Endpoint? {
        var raw = environment["MAD_FIXTURE_URL"]
        if raw == nil, let i = arguments.firstIndex(of: "-fixtureURL"), i + 1 < arguments.count { raw = arguments[i + 1] }
        guard var s = raw?.trimmingCharacters(in: .whitespacesAndNewlines), !s.isEmpty else { return nil }
        while s.hasSuffix("/") { s.removeLast() }
        guard let url = URL(string: s), url.scheme == "http" || url.scheme == "https", url.host != nil else { return nil }
        return Endpoint(base: url, token: token)
    }
}
