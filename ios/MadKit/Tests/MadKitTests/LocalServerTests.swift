import XCTest
@testable import MadKit

private func tempDir() throws -> URL {
    let d = FileManager.default.temporaryDirectory.appendingPathComponent("madkit-" + UUID().uuidString)
    try FileManager.default.createDirectory(at: d, withIntermediateDirectories: true)
    addTeardownBlockHelper(d)
    return d
}

private var cleanup: [URL] = []
private func addTeardownBlockHelper(_ d: URL) { cleanup.append(d) }

final class LocalServerTests: XCTestCase {
    override func tearDown() {
        for d in cleanup { try? FileManager.default.removeItem(at: d) }
        cleanup = []
        super.tearDown()
    }

    func testTheStateDirectoryFollowsXDGOrHome() {
        let home = URL(fileURLWithPath: "/Users/u")
        XCTAssertEqual(LocalServer.stateDirectory(environment: ["XDG_STATE_HOME": "/st"], home: home).path, "/st/mad")
        XCTAssertEqual(LocalServer.stateDirectory(environment: [:], home: home).path, "/Users/u/.local/state/mad")
        XCTAssertEqual(LocalServer.stateDirectory(environment: ["XDG_STATE_HOME": ""], home: home).path, "/Users/u/.local/state/mad")
    }

    func testThePhoneTokenIsReadAndTrimmed() throws {
        let dir = try tempDir()
        XCTAssertNil(LocalServer.phoneToken(stateDirectory: dir), "no file, no token")
        try "0123456789abcdef0123456789abcdef\n".write(to: dir.appendingPathComponent("phone_token"), atomically: true, encoding: .utf8)
        XCTAssertEqual(LocalServer.phoneToken(stateDirectory: dir), "0123456789abcdef0123456789abcdef")
        try "  \n".write(to: dir.appendingPathComponent("phone_token"), atomically: true, encoding: .utf8)
        XCTAssertNil(LocalServer.phoneToken(stateDirectory: dir), "a blank file is no token")
    }

    func testTheReadyLineGivesTheBaseAndTheToken() {
        XCTAssertEqual(LocalServer.parseReadyLine("http://127.0.0.1:52445/#token=abc123\n"),
                       Endpoint(base: URL(string: "http://127.0.0.1:52445")!, token: "abc123"))
        XCTAssertNil(LocalServer.parseReadyLine("mad: serving the phone on 127.0.0.1:17484"))
        XCTAssertNil(LocalServer.parseReadyLine("http://127.0.0.1:1/"))
        XCTAssertNil(LocalServer.parseReadyLine("http://127.0.0.1:1/#token="))
        XCTAssertNil(LocalServer.parseReadyLine("ftp://127.0.0.1/#token=x"))
        XCTAssertNil(LocalServer.parseReadyLine(""))
    }

    func testEndpointMakesAClientForItsServer() {
        let e = Endpoint(base: URL(string: "http://127.0.0.1:9")!, token: "t")
        XCTAssertEqual(e.client().request("/api/deck").url?.absoluteString, "http://127.0.0.1:9/api/deck")
        XCTAssertEqual(e.client().request("/api/deck").value(forHTTPHeaderField: "Authorization"), "Bearer t")
    }

    func testMadIsFoundByPathOrOnPATH() throws {
        let dir = try tempDir()
        let bin = dir.appendingPathComponent("mad")
        try "#!/bin/sh\n".write(to: bin, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: bin.path)
        XCTAssertEqual(LocalServer.findMad(path: bin.path)?.path, bin.path)
        XCTAssertNil(LocalServer.findMad(path: dir.appendingPathComponent("nope").path), "a set path that is no program is not replaced by PATH")
        XCTAssertEqual(LocalServer.findMad(environment: ["PATH": "/nonexistent:" + dir.path])?.path, bin.path)
        XCTAssertNotEqual(LocalServer.findMad(environment: ["PATH": "/nonexistent"])?.path, bin.path)
        // A directory named mad is not a program.
        let dir2 = try tempDir()
        try FileManager.default.createDirectory(at: dir2.appendingPathComponent("mad"), withIntermediateDirectories: true)
        XCTAssertNotEqual(LocalServer.findMad(environment: ["PATH": dir2.path])?.path, dir2.appendingPathComponent("mad").path)
    }

    func testFixtureHookReadsTheEnvironmentOrTheArgument() {
        XCTAssertEqual(FixtureHook.endpoint(environment: ["MAD_FIXTURE_URL": "http://127.0.0.1:5000/"], arguments: [])?.base.absoluteString,
                       "http://127.0.0.1:5000")
        XCTAssertEqual(FixtureHook.endpoint(environment: [:], arguments: ["x", "-fixtureURL", "http://127.0.0.1:6000"])?.base.absoluteString,
                       "http://127.0.0.1:6000")
        XCTAssertEqual(FixtureHook.endpoint(environment: ["MAD_FIXTURE_URL": "http://127.0.0.1:1"], arguments: ["-fixtureURL", "http://127.0.0.1:2"])?.base.port, 1,
                       "the environment wins")
        XCTAssertEqual(FixtureHook.endpoint(environment: ["MAD_FIXTURE_URL": "http://h:1"], arguments: [])?.token.count, 32, "a token that looks like one")
        XCTAssertNil(FixtureHook.endpoint(environment: [:], arguments: []))
        XCTAssertNil(FixtureHook.endpoint(environment: ["MAD_FIXTURE_URL": "not a url"], arguments: []))
        XCTAssertNil(FixtureHook.endpoint(environment: [:], arguments: ["-fixtureURL"]))
    }

    #if os(macOS)
    private func script(_ body: String) throws -> URL {
        let url = try tempDir().appendingPathComponent("fake-mad")
        try ("#!/bin/sh\n" + body + "\n").write(to: url, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: url.path)
        return url
    }

    func testTheLauncherReadsTheURLTheServerPrintsAndStopsIt() async throws {
        let fake = try script("echo http://127.0.0.1:43210/#token=feedbeef\nexec sleep 60")
        let launcher = ServerLauncher()
        let e = try await launcher.start(binary: fake, arguments: [], timeout: 10)
        XCTAssertEqual(e, Endpoint(base: URL(string: "http://127.0.0.1:43210")!, token: "feedbeef"))
        XCTAssertTrue(launcher.isRunning)
        launcher.stop()
        XCTAssertFalse(launcher.isRunning)
        launcher.stop() // idempotent
    }

    func testTheLauncherTakesTMUXOutOfTheEnvironment() async throws {
        setenv("TMUX", "/tmp/x,1,0", 1)
        defer { unsetenv("TMUX") }
        let fake = try script("echo http://127.0.0.1:1/#token=${TMUX:-none}\nexec sleep 60")
        let launcher = ServerLauncher()
        let e = try await launcher.start(binary: fake, arguments: [], timeout: 10)
        launcher.stop()
        XCTAssertEqual(e.token, "none", "mad refuses to start a window inside its own deck")
    }

    func testTheLauncherFailsClearlyWhenTheServerSaysNothingUseful() async throws {
        let exits = try script("exit 1")
        do { _ = try await ServerLauncher().start(binary: exits, arguments: [], timeout: 5); XCTFail("expected a failure") } catch {
            XCTAssertEqual(error as? LocalServer.LaunchError, .failed("it exited at once"))
        }
        let chatter = try script("echo hello there\nexec sleep 60")
        let l = ServerLauncher()
        do { _ = try await l.start(binary: chatter, arguments: [], timeout: 5); XCTFail("expected a failure") } catch {
            XCTAssertEqual(error as? LocalServer.LaunchError, .failed("unexpected output: hello there"))
        }
        XCTAssertFalse(l.isRunning, "a server that did not say where it is, is stopped")
    }

    func testTheLauncherTimesOutOnASilentServer() async throws {
        let silent = try script("exec sleep 60")
        let l = ServerLauncher()
        do { _ = try await l.start(binary: silent, arguments: [], timeout: 1); XCTFail("expected a timeout") } catch {
            XCTAssertEqual(error as? LocalServer.LaunchError, .timeout)
        }
        XCTAssertFalse(l.isRunning)
    }

    func testTheLifelineIsNotInheritedByOtherChildren() async throws {
        let fake = try script("echo http://127.0.0.1:43210/#token=feedbeef\ncat >/dev/null")
        let launcher = ServerLauncher()
        _ = try await launcher.start(binary: fake, arguments: [], timeout: 10)
        defer { launcher.stop() }
        let fd = try XCTUnwrap(launcher.lifelineDescriptor)
        XCTAssertNotEqual(fcntl(fd, F_GETFD) & FD_CLOEXEC, 0, "the write end must close on exec")
    }

    /// The app can be killed without a chance to stop its server: the server must go with it.
    func testTheServerExitsWhenTheAppIsKilled() throws {
        // A server that ends when its stdin does, as `mad desktop -exit-on-stdin-eof` does.
        let fake = try script("echo http://127.0.0.1:43210/#token=feedbeef\ncat >/dev/null")
        let probeURL = Bundle(for: LocalServerTests.self).bundleURL.deletingLastPathComponent()
            .appendingPathComponent("launcher-probe")
        try XCTSkipUnless(FileManager.default.isExecutableFile(atPath: probeURL.path), "launcher-probe is not built")
        let app = Process()
        app.executableURL = probeURL
        app.arguments = [fake.path]
        let out = Pipe()
        app.standardOutput = out
        try app.run()
        defer { if app.isRunning { app.terminate() } }
        let line = String(decoding: out.fileHandleForReading.availableData, as: UTF8.self)
        guard let pid = Int32(line.trimmingCharacters(in: .whitespacesAndNewlines)) else { return XCTFail("probe said: \(line)") }
        XCTAssertEqual(kill(pid, 0), 0, "the server runs while the app does")
        kill(app.processIdentifier, SIGKILL)
        app.waitUntilExit()
        var gone = false
        for _ in 0..<100 where !gone {
            Thread.sleep(forTimeInterval: 0.05)
            gone = kill(pid, 0) != 0
        }
        XCTAssertTrue(gone, "the server is still running 5 s after the app was killed")
    }
    #endif
}
