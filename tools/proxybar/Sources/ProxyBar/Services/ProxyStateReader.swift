import Foundation

/// 读取系统代理真实状态（networksetup -get*）。
/// 读取失败降级为关闭态（宽容处理，绝不崩溃）。
struct ProxyStateReader: Sendable {
    let runner: CommandRunning

    init(runner: CommandRunning) {
        self.runner = runner
    }

    func read(service: String) async -> SystemProxyState {
        async let httpOut = output(NetworkSetup.getArgs(.http, service: service))
        async let httpsOut = output(NetworkSetup.getArgs(.https, service: service))
        async let socksOut = output(NetworkSetup.getArgs(.socks, service: service))
        async let pacOut = output(NetworkSetup.getArgs(.pac, service: service))
        async let bypassOut = output(NetworkSetup.getBypassArgs(service: service))
        return await SystemProxyState(
            http: NetworkSetup.parseTypeState(httpOut),
            https: NetworkSetup.parseTypeState(httpsOut),
            socks: NetworkSetup.parseTypeState(socksOut),
            pac: NetworkSetup.parsePACState(pacOut),
            bypass: NetworkSetup.parseBypass(bypassOut)
        )
    }

    func read(services: [String]) async -> [String: SystemProxyState] {
        await withTaskGroup(of: (String, SystemProxyState).self) { group in
            for service in services {
                group.addTask { (service, await self.read(service: service)) }
            }
            var result: [String: SystemProxyState] = [:]
            for await (name, state) in group {
                result[name] = state
            }
            return result
        }
    }

    private func output(_ args: [String]) async -> String {
        do {
            let result = try await runner.run(args)
            return result.exitCode == 0 ? result.stdout : ""
        } catch {
            return ""
        }
    }
}