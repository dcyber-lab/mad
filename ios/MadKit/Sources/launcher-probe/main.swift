// A stand-in for the Mac app, for the test that kills it with SIGKILL: launches the server it is
// given (a path), prints "<pid>" of that server, then waits to be killed.
#if os(macOS)
import Foundation
import MadKit

let launcher = ServerLauncher()
_ = try await launcher.start(binary: URL(fileURLWithPath: CommandLine.arguments[1]), arguments: [], timeout: 10)
print(launcher.processIdentifier ?? -1)
fflush(stdout)
try await Task.sleep(nanoseconds: 120 * 1_000_000_000)
#else
print("macOS only")
#endif
