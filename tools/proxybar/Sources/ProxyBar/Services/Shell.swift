import Foundation

struct CommandResult: Sendable {
    var args: [String]
    var exitCode: Int32
    var stdout: String
    var stderr: String
}

/// 子进程边界协议：单测用 FakeRunner 注入，真实实现跑 networksetup。
protocol CommandRunning: Sendable {
    func run(_ args: [String]) async throws -> CommandResult
}

/// 真实实现：Process  confined 在 detached task 内，只有 Sendable 结果跨界（Swift 6 安全）。
/// 外部系统边界，由手动 E2E 验证（单测不起真实子进程）。
struct NetworkSetupRunner: CommandRunning {
    func run(_ args: [String]) async throws -> CommandResult {
        try await Task.detached(priority: .userInitiated) {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: "/usr/sbin/networksetup")
            process.arguments = args
            let outPipe = Pipe()
            let errPipe = Pipe()
            process.standardOutput = outPipe
            process.standardError = errPipe
            try process.run()
            // networksetup 输出远小于管道缓冲，先读尽再 wait 不会死锁。
            let outData = outPipe.fileHandleForReading.readDataToEndOfFile()
            let errData = errPipe.fileHandleForReading.readDataToEndOfFile()
            process.waitUntilExit()
            return CommandResult(
                args: args,
                exitCode: process.terminationStatus,
                stdout: String(decoding: outData, as: UTF8.self),
                stderr: String(decoding: errData, as: UTF8.self)
            )
        }.value
    }
}