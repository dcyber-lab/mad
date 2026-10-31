// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "MadKit",
    platforms: [.iOS(.v17), .macOS(.v14)],
    products: [.library(name: "MadKit", targets: ["MadKit"])],
    targets: [
        .target(name: "MadKit"),
        // A stand-in for the app, for a test that kills it: it launches a server and waits.
        .executableTarget(name: "launcher-probe", dependencies: ["MadKit"]),
        .testTarget(name: "MadKitTests", dependencies: ["MadKit"], resources: [.copy("Fixtures")]),
    ]
)
