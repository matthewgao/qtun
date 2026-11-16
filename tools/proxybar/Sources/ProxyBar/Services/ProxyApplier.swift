import Foundation

struct ApplyFailure: Sendable, Equatable {
    var service: String
    var args: [String]
    var exitCode: Int32
    var message: String
}

/// actor 串行应用：逐条执行、收集失败、绝不中途放弃。
actor ProxyApplier {
    private let runner: CommandRunning

    init(runner: CommandRunning) {
        self.runner = runner
    }

    func apply(profile: ProxyProfile?, services: [String]) async -> [ApplyFailure] {
        var failures: [ApplyFailure] = []
        for service in services {
            for args in NetworkSetup.applyCommands(profile: profile, service: service) {
                do {
                    let result = try await runner.run(args)
                    if result.exitCode != 0 {
                        let message = result.stderr.trimmed.isEmpty ? result.stdout.trimmed : result.stderr.trimmed
                        failures.append(ApplyFailure(service: service, args: args, exitCode: result.exitCode, message: message))
                    }
                } catch {
                    failures.append(ApplyFailure(service: service, args: args, exitCode: -1, message: error.localizedDescription))
                }
            }
        }
        return failures
    }
}