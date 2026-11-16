import Foundation

/// networksetup 的纯参数构造器与纯输出解析器（可单测核心，无 I/O）。
/// 所有 flag 与输出格式已在 macOS 26.7.2 实测验证。
enum NetworkSetup {

    // MARK: - 读取命令

    static func getArgs(_ kind: ProxyKind, service: String) -> [String] {
        let flag: String
        switch kind {
        case .http: flag = "-getwebproxy"
        case .https: flag = "-getsecurewebproxy"
        case .socks: flag = "-getsocksfirewallproxy"
        case .pac: flag = "-getautoproxyurl"
        }
        return [flag, service]
    }

    static func getBypassArgs(service: String) -> [String] {
        ["-getproxybypassdomains", service]
    }

    // MARK: - 输出解析

    /// 解析 -getwebproxy / -getsecurewebproxy / -getsocksfirewallproxy 输出。
    /// 禁用时 host/port 残留值原样保留（匹配器负责忽略它们）。
    static func parseTypeState(_ output: String) -> ProxyTypeState {
        var enabled = false
        var host = ""
        var port = 0
        forEachField(in: output) { key, value in
            switch key {
            case "Enabled": enabled = (value == "Yes")
            case "Server": host = value
            case "Port": port = Int(value) ?? 0
            default: break  // 忽略 Authenticated Proxy Enabled 等
            }
        }
        return ProxyTypeState(enabled: enabled, host: host, port: port)
    }

    /// 解析 -getautoproxyurl 输出；"(null)" 与空串视为 nil。
    static func parsePACState(_ output: String) -> PACState {
        var enabled = false
        var url: String?
        forEachField(in: output) { key, value in
            switch key {
            case "Enabled": enabled = (value == "Yes")
            case "URL": url = (value.isEmpty || value == "(null)") ? nil : value
            default: break
            }
        }
        return PACState(enabled: enabled, url: url)
    }

    /// 解析 -getproxybypassdomains 输出：每行一条域名；空列表提示语视为 []。
    static func parseBypass(_ output: String) -> [String] {
        let whole = output.trimmed
        if whole.isEmpty || whole.hasPrefix("There aren't any bypass domains") {
            return []
        }
        return output.split(whereSeparator: \.isNewline)
            .map { String($0).trimmed }
            .filter { !$0.isEmpty }
    }

    /// 按行拆分 "Key: Value" 字段（首个冒号分割，容忍 CRLF 与多余空白）。
    private static func forEachField(in output: String, _ body: (String, String) -> Void) {
        for line in output.split(whereSeparator: \.isNewline) {
            let parts = line.split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
            guard parts.count == 2 else { continue }
            body(String(parts[0]).trimmed, String(parts[1]).trimmed)
        }
    }

    // MARK: - 写入命令

    /// 应用配置到单个服务的完整命令序列；profile 为 nil 表示直连。
    /// 顺序：http → https → socks → pac → bypass。
    /// 配置中存在的类型用值设置器（自带开启）；不存在的类型显式关闭（值保留）。
    /// 直连只关四类型，不动 bypass。
    static func applyCommands(profile: ProxyProfile?, service: String) -> [[String]] {
        guard let p = profile else {
            return [
                ["-setwebproxystate", service, "off"],
                ["-setsecurewebproxystate", service, "off"],
                ["-setsocksfirewallproxystate", service, "off"],
                ["-setautoproxystate", service, "off"],
            ]
        }
        var cmds: [[String]] = []
        if let e = p.http {
            cmds.append(["-setwebproxy", service, e.host, String(e.port), "off"])
        } else {
            cmds.append(["-setwebproxystate", service, "off"])
        }
        if let e = p.https {
            cmds.append(["-setsecurewebproxy", service, e.host, String(e.port), "off"])
        } else {
            cmds.append(["-setsecurewebproxystate", service, "off"])
        }
        if let e = p.socks {
            cmds.append(["-setsocksfirewallproxy", service, e.host, String(e.port), "off"])
        } else {
            cmds.append(["-setsocksfirewallproxystate", service, "off"])
        }
        if let url = p.pacURL {
            cmds.append(["-setautoproxyurl", service, url])
        } else {
            cmds.append(["-setautoproxystate", service, "off"])
        }
        // 空列表传字面量 "Empty" 清空；总是写以保证确定性。
        cmds.append(["-setproxybypassdomains", service] + (p.bypass.isEmpty ? ["Empty"] : p.bypass))
        return cmds
    }
}