// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "ProxyBar",
    platforms: [.macOS(.v14)],
    targets: [
        .executableTarget(
            name: "ProxyBar",
            path: "Sources/ProxyBar"
        ),
        .testTarget(
            name: "ProxyBarTests",
            dependencies: ["ProxyBar"],
            path: "Tests/ProxyBarTests"
        ),
    ]
)