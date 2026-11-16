import Foundation

/// 网络服务枚举：纯解析 + 目标规则。
enum ServiceEnumerator {

    /// 合并 -listallnetworkservices（名称/禁用，首行为说明头）
    /// 与 -listnetworkserviceorder（硬件口/设备，禁用服务带 (*) 前缀）。
    /// 保持 listAll 的顺序；order 缺失的服务 hardware/device 留空。
    static func parse(listAll: String, order: String) -> [NetworkService] {
        var info: [String: (hardware: String, device: String)] = [:]
        var currentName: String?
        for rawLine in order.split(whereSeparator: \.isNewline) {
            let line = rawLine.trimmed
            if line.isEmpty { continue }
            if let m = line.firstMatch(of: /^(?:\(\*\)\s*)?\((\d+)\)\s+(.+)$/) {
                currentName = String(m.2)
            } else if let m = line.firstMatch(of: /^\(Hardware Port: (.*), Device: (.*)\)$/),
                      let name = currentName {
                info[name] = (String(m.1), String(m.2))
                currentName = nil
            }
        }

        var result: [NetworkService] = []
        for (index, rawLine) in listAll.split(whereSeparator: \.isNewline).enumerated() {
            if index == 0 { continue }  // 说明头
            let line = rawLine.trimmed
            if line.isEmpty { continue }
            let disabled = line.hasPrefix("*")
            let name = disabled ? String(line.dropFirst()).trimmed : line
            result.append(NetworkService(
                name: name,
                hardwarePort: info[name]?.hardware ?? "",
                device: info[name]?.device ?? "",
                enabled: !disabled
            ))
        }
        return result
    }

    /// 默认目标：启用 且 非 iBridge（实测报 Enabled，必须显式排除）且 设备非 bridge*。
    static func isDefaultTarget(_ service: NetworkService) -> Bool {
        service.enabled
            && service.hardwarePort != "iBridge"
            && !service.device.hasPrefix("bridge")
    }

    /// 用户覆盖优先于默认规则。
    static func targets(_ services: [NetworkService], overrides: [String: Bool]) -> [NetworkService] {
        services.filter { overrides[$0.name] ?? isDefaultTarget($0) }
    }
}