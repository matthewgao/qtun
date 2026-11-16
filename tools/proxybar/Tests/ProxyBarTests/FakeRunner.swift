import Foundation
@testable import ProxyBar

/// 测试替身：记录执行的命令，按 args 键返回罐装输出/失败。
/// 只模拟外部边界（子进程），解析/构造/编排全部走真实代码。
final class FakeRunner: CommandRunning, @unchecked Sendable {
    private let lock = NSLock()
    private var _executed: [[String]] = []

    /// key = args.joined(separator: " ")
    var outputs: [String: String] = [:]
    var failures: Set<String> = []

    var executed: [[String]] { lock.withLock { _executed } }

    func run(_ args: [String]) async throws -> CommandResult {
        lock.withLock { _executed.append(args) }
        let key = args.joined(separator: " ")
        let failed = failures.contains(key)
        return CommandResult(
            args: args,
            exitCode: failed ? 1 : 0,
            stdout: outputs[key] ?? "",
            stderr: failed ? "boom" : ""
        )
    }
}